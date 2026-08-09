# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, networks, and volumes while always protecting Tengiz-managed containers (label `tengiz-app=*`) and `tengiz-apps/*` images so scale-to-zero cold starts and rollbacks keep working.

**Architecture:** New `runtime.Cleanup(ctx, opts) (*CleanupReport, error)` method on the existing `runtime.Manager` interface (exec-based `dockerRuntime` impl + stub). The implementation composes granular, typed `docker ... prune` commands instead of a blind `docker system prune -a`, so protection rules are explicit and testable: containers are pruned only with `--filter label!=tengiz-app` (verified to keep stopped Tengiz containers — the critical scale-to-zero case), and `--all` image pruning never touches the `tengiz-apps/*` namespace (rollback protection), computed by listing images and excluding protected refs. Pure helper functions carry the command construction so the docker exec calls are covered by fake-`docker`-in-`PATH` tests that run in CI without a real daemon. The Cobra CLI command surfaces a report of every executed step and supports `--dry-run`, `--yes`, and a per-category flag set.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), standard library only — no new dependencies.

## Global Constraints

- New command: `tengiz cleanup [--all] [--volumes] [--dry-run] [--yes] [--no-containers] [--no-networks] [--no-images]`
- ALWAYS pass `--label label!=tengiz-app` when pruning containers (protects stopped scale-to-zero containers that must cold-start)
- NEVER remove `tengiz-apps/*` images, even with `--all`
- `--all` means "prune all unused images except `tengiz-apps/*`", **not** `docker image prune -a` (that would delete rollback images)
- Volumes are only pruned when `--volumes` is given (opt-in, most destructive)
- No new external dependencies (go.mod untouched)
- Use existing `runtime.Manager` interface; all existing mock types that implement it MUST gain the new `Cleanup` method or compilation fails
- Confirm prompt appears on TTY only; `--yes/-y` and non-TTY stdin skip it
- Every task must pass its tests (`go test ./internal/... -v -count=1`), and the final task runs `go build -o tengiz .`, `go vet ./...` and `go test ./... -v -count=1`
- Update `README.md`, `AGENTS.md` CLI reference, and mark feature #6 `✅ Implemented` in `docs/FUTURES_FEATURES.md`
- Existing tests must keep passing unchanged except for the three mock files that gain the new interface method

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` (modify) | `CleanupOptions`, `CleanupStep`, `CleanupReport` types; pure command builders (`containerPruneArgs`, `networkPruneArgs`, `volumePruneArgs`, `imagePruneArgs`, `rmiArgs`, `allImageListArgs`, `protectedImageListArgs`, `selectImagePruneCandidates`); `dockerRuntime.Cleanup` implementation |
| `internal/runtime/runtime.go` (modify) | Add `Cleanup(ctx, opts)` to `Manager` interface + `stubManager` implementation |
| `internal/runtime/cleanup_test.go` (modify) | Unit tests for builders + candidate selection; fake-docker integration tests for `dockerRuntime.Cleanup` |
| `internal/proxy/proxy_test.go` (modify) | Add `Cleanup` method to `mockRuntime` so it still satisfies `runtime.Manager` |
| `internal/idle/idle_test.go` (modify) | Add `Cleanup` method to `mockRuntime` so it still satisfies `runtime.Manager` |
| `internal/cli/root_test.go` (modify) | Add `Cleanup` method to `mockRTForDeploy` so it still satisfies `runtime.Manager` |
| `internal/cli/cleanup.go` (create) | The `tengiz cleanup` Cobra command + `confirmCleanup()` help |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/cleanup_test.go` (create) | Registration/flags test, dry-run-no-docker test, nothing-selected test |
| `README.md` (modify) | New "`tengiz cleanup`" section in CLI Reference |
| `AGENTS.md` (modify) | Add `tengiz cleanup` line to the CLI list |
| `docs/FUTURES_FEATURES.md` (modify) | Mark #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Cleanup types + pure command-builder helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types + pure helper functions
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (uses `labelKey` constant already defined in `internal/runtime/docker.go:76`)
- Produces:
  - `CleanupOptions{ All, Volumes, Containers, Networks, Images, DryRun bool }`
  - `CleanupReport{ DryRun bool; Steps []CleanupStep }` and `CleanupStep{ Category, Command, Output string }`
  - `containerPruneArgs() []string` → `["container","prune","--force","--filter","label!=tengiz-app"]`
  - `networkPruneArgs() []string` → `["network","prune","--force"]`
  - `volumePruneArgs() []string` → `["volume","prune","--force"]`
  - `imagePruneArgs() []string` → `["image","prune","--force"]`
  - `rmiArgs(ref string) []string` → `["rmi","--force",ref]`
  - `allImageListArgs() []string` → `["images","--format","{{.Repository}}:{{.Tag}}"]`
  - `protectedImageListArgs() []string` → `["images","--filter","reference=tengiz-apps/*","--format","{{.Repository}}:{{.Tag}}"]`
  - `selectImagePruneCandidates(allLines, protectedLines string) []string`

- [ ] **Step 1: Write the failing tests**

Replace the entire contents of `internal/runtime/cleanup_test.go` with:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestContainerPruneArgs(t *testing.T) {
	got := containerPruneArgs()
	want := []string{"container", "prune", "--force", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneArgs() = %v, want %v", got, want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	got := networkPruneArgs()
	want := []string{"network", "prune", "--force"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneArgs() = %v, want %v", got, want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	got := volumePruneArgs()
	want := []string{"volume", "prune", "--force"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneArgs() = %v, want %v", got, want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	got := imagePruneArgs()
	want := []string{"image", "prune", "--force"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("imagePruneArgs() = %v, want %v", got, want)
	}
}

func TestRmiArgs(t *testing.T) {
	got := rmiArgs("ubuntu:latest")
	want := []string{"rmi", "--force", "ubuntu:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rmiArgs() = %v, want %v", got, want)
	}
}

func TestAllImageListArgs(t *testing.T) {
	got := allImageListArgs()
	want := []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("allImageListArgs() = %v, want %v", got, want)
	}
}

func TestProtectedImageListArgs(t *testing.T) {
	got := protectedImageListArgs()
	want := []string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("protectedImageListArgs() = %v, want %v", got, want)
	}
}

func TestSelectImagePruneCandidates(t *testing.T) {
	all := "ubuntu:latest\nalpine:latest\ntengiz-apps/hello:prod-1\ntengiz-apps/hello:prod-latest\n"
	protected := "tengiz-apps/hello:prod-1\ntengiz-apps/hello:prod-latest\n"
	got := selectImagePruneCandidates(all, protected)
	want := []string{"ubuntu:latest", "alpine:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagePruneCandidates() = %v, want %v", got, want)
	}
}

func TestSelectImagePruneCandidatesSkipsDanglingAndEmpty(t *testing.T) {
	got := selectImagePruneCandidates("<none>:<none>\n\nubuntu:latest\n", "")
	want := []string{"ubuntu:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagePruneCandidates() = %v, want %v", got, want)
	}
}
```

Note: the old `TestStubRemoveImage` and `TestStubKeepLastNImages` tests are removed (they only covered the stub, which is covered again in Task 2).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestContainerPruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestImagePruneArgs|TestRmiArgs|TestAllImageListArgs|TestProtectedImageListArgs|TestSelectImagePruneCandidates" -v -count=1`

Expected: FAIL with `undefined: containerPruneArgs`, `undefined: selectImagePruneCandidates`, etc.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
type CleanupOptions struct {
	All        bool
	Volumes    bool
	Containers bool
	Networks   bool
	Images     bool
	DryRun     bool
}

type CleanupStep struct {
	Category string
	Command  string
	Output   string
}

type CleanupReport struct {
	DryRun bool
	Steps  []CleanupStep
}

const imageProtectFilter = "reference=tengiz-apps/*"

func containerPruneArgs() []string {
	return []string{"container", "prune", "--force", "--filter", "label!=" + labelKey}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "--force"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "--force"}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "--force"}
}

func rmiArgs(ref string) []string {
	return []string{"rmi", "--force", ref}
}

func allImageListArgs() []string {
	return []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}
}

func protectedImageListArgs() []string {
	return []string{"images", "--filter", imageProtectFilter, "--format", "{{.Repository}}:{{.Tag}}"}
}

// selectImagePruneCandidates returns every image reference listed in allLines
// that is not listed in protectedLines. Dangling entries (Repository "<none>")
// and blank lines are never returned.
func selectImagePruneCandidates(allLines, protectedLines string) []string {
	protected := make(map[string]struct{})
	for _, line := range strings.Split(protectedLines, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, ":<none>") {
			continue
		}
		protected[ref] = struct{}{}
	}
	var candidates []string
	for _, line := range strings.Split(allLines, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, ":<none>") {
			continue
		}
		if _, ok := protected[ref]; ok {
			continue
		}
		candidates = append(candidates, ref)
	}
	return candidates
}
```

(`strings` is already imported in `cleanup.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestContainerPruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestImagePruneArgs|TestRmiArgs|TestAllImageListArgs|TestProtectedImageListArgs|TestSelectImagePruneCandidates" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup types and docker prune command builders"
```

---

### Task 2: Wire `Cleanup` into the `runtime.Manager` interface + implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Cleanup` to `Manager` interface + `stubManager`
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`
- Modify: `internal/runtime/cleanup_test.go` — fake-docker integration tests
- Modify: `internal/proxy/proxy_test.go` — grant `mockRuntime.Cleanup`
- Modify: `internal/idle/idle_test.go` — grant `mockRuntime.Cleanup`
- Modify: `internal/cli/root_test.go` — grant `mockRTForDeploy.Cleanup`

**Interfaces:**
- Consumes: types + builders from Task 1
- Produces: `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — later tasks and mock/fake implementations call exactly this signature

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
// writeFakeDocker writes a fake `docker` shell script into a temp dir that:
//   - logs every invocation to callLog (one line per call, space-joined args)
//   - emits canned prune output for container/network/volume/image prune
//   - lists images unconditionally (protected list filter returns only tengiz-apps refs)
//   - answers rmi with "Deleted: <ref>"
// Its dir is returned for placement on PATH.
func writeFakeDocker(t *testing.T, callLog string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "CALL $*" >> "` + callLog + `"
case "$1" in
  container)
    echo "Deleted Containers:"
    echo "dead-1"
    echo "Total reclaimed space: 0B"
    ;;
  image)
    echo "Deleted Images:"
    echo "img-1"
    echo "Total reclaimed space: 0B"
    ;;
  network)
    echo "Total reclaimed space: 0B"
    ;;
  volume)
    echo "Deleted Volumes:"
    echo "vol-1"
    echo "Total reclaimed space: 11.5MB"
    ;;
  images)
    if echo "$*" | grep -q -- "reference=tengiz-apps"; then
      echo "tengiz-apps/hello:prod-1"
    else
      echo "ubuntu:latest"
      echo "alpine:latest"
      echo "tengiz-apps/hello:prod-1"
    fi
    ;;
  rmi)
    echo "Deleted: $2"
    ;;
  *)
    echo "unexpected: $*" >&2
    exit 9
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	return dir
}

func withFakeDockerPath(t *testing.T, dir string) {
	t.Helper()
	old := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	t.Cleanup(func() { os.Setenv("PATH", old) })
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("stub Cleanup() error = %v", err)
	}
	if !report.DryRun {
		t.Error("stub Cleanup() DryRun = false, want true")
	}
}

func TestDockerCleanupRunsGranularPrunes(t *testing.T) {
	dir := t.TempDir()
	callLog := filepath.Join(dir, "calls.log")
	withFakeDockerPath(t, writeFakeDocker(t, callLog))

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker: %v", err)
	}

	report, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		Volumes:    true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(report.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d: %+v", len(report.Steps), report.Steps)
	}

	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	for _, want := range []string{
		"CALL container prune --force --filter label!=tengiz-app",
		"CALL image prune --force",
		"CALL network prune --force",
		"CALL volume prune --force",
	} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("call log missing %q:\n%s", want, logged)
		}
	}
}

func TestDockerCleanupAllProtectsTengizAppImages(t *testing.T) {
	dir := t.TempDir()
	callLog := filepath.Join(dir, "calls.log")
	withFakeDockerPath(t, writeFakeDocker(t, callLog))

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker: %v", err)
	}

	report, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		All:        true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(report.Steps) < 4 {
		t.Fatalf("expected >=4 steps, got %d", len(report.Steps))
	}

	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	s := string(logged)
	for _, want := range []string{
		"CALL rmi --force ubuntu:latest",
		"CALL rmi --force alpine:latest",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("call log missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "rmi --force tengiz-apps/") {
		t.Errorf("protected image rmi attempted:\n%s", s)
	}
}

func TestDockerCleanupDryRunDoesNotInvokeDocker(t *testing.T) {
	dir := t.TempDir()
	callLog := filepath.Join(dir, "calls.log")
	withFakeDockerPath(t, writeFakeDocker(t, callLog))

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker: %v", err)
	}

	report, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		All:        true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(report.Steps) != 4 {
		t.Fatalf("expected 4 dry-run steps, got %d: %+v", len(report.Steps), report.Steps)
	}
	for _, step := range report.Steps {
		if step.Output == "" || step.Command == "" {
			t.Errorf("dry-run step missing Command/Output: %+v", step)
		}
	}
	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	if strings.TrimSpace(string(logged)) != "" {
		t.Errorf("dry-run invoked docker:\n%s", logged)
	}
}
```

The test file needs imports `context`, `os`, `path/filepath`, `strings` (and `testing`). Add them at the top of `internal/runtime/cleanup_test.go`:

```go
import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: FAIL — compile error because `runtime.Cleanup` does not exist yet. (Adding the new method to the existing mocks is required for the whole repo to compile — see Step 5.)

- [ ] **Step 3: Add the method to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, after the `Run` line in the `Manager` interface (line 48), add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

In the same file, after `stubManager.Run` (line 121), add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Implement `dockerRuntime.Cleanup`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}

	runSteps := func(category string, args []string) {
		step := CleanupStep{
			Category: category,
			Command:  "docker " + strings.Join(args, " "),
		}
		if opts.DryRun {
			step.Output = "(dry run) would run: " + step.Command
			report.Steps = append(report.Steps, step)
			return
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		step.Output = strings.TrimSpace(string(out))
		if err != nil {
			step.Output += fmt.Sprintf("\ndocker error: %v", err)
		}
		report.Steps = append(report.Steps, step)
	}

	if opts.Containers {
		runSteps("containers", containerPruneArgs())
	}
	if opts.Images {
		runSteps("images", imagePruneArgs())
	}
	if opts.All {
		if opts.DryRun {
			report.Steps = append(report.Steps, CleanupStep{
				Category: "images",
				Command:  "all unused images",
				Output:   "(dry run) would prune all unused images; tengiz-apps/* images are always protected",
			})
		} else {
			allOut, err := exec.CommandContext(ctx, "docker", allImageListArgs()...).CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("docker image list: %w", err)
			}
			protOut, err := exec.CommandContext(ctx, "docker", protectedImageListArgs()...).CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("docker image list tengiz-apps: %w", err)
			}
			for _, ref := range selectImagePruneCandidates(string(allOut), string(protOut)) {
				runSteps("images", rmiArgs(ref))
			}
		}
	}
	if opts.Networks {
		runSteps("networks", networkPruneArgs())
	}
	if opts.Volumes {
		runSteps("volumes", volumePruneArgs())
	}
	return report, nil
}
```

- [ ] **Step 5: Grant `Cleanup` to the three existing mock implementations**

In `internal/proxy/proxy_test.go`, after line 35 (`Run`), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

In `internal/idle/idle_test.go`, after the mock's `Run` method, add the identical method (confirm the mock's type name is `mockRuntime` first with `grep -n "type mockRuntime struct" internal/idle/idle_test.go`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

In `internal/cli/root_test.go`, after `mockRTForDeploy.Run` (line 100), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: all PASS, including `TestDockerCleanupRuns...GranularPrunes`, `TestDockerCleanupAllProtectsTengizAppImages`, `TestDockerCleanupDryRunDoesNotInvokeDocker`

- [ ] **Step 7: Verify the whole module compiles and tests pass**

Run: `go build ./...`

Expected: builds without error (proves every mock still satisfies `runtime.Manager`)

Run: `go vet ./...`

Expected: no issues

Run: `go test ./... -v -count=1`

Expected: all PASS. (proxy tests take ~2s each due to TCP dial timeouts; idle tests sleep with 50ms granularity — this is expected and not a failure.)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add runtime.Manager.Cleanup for docker housekeeping"
```

---

### Task 3: CLI command `tengiz cleanup`

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — register command + flags in `init()`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()`, `getEnv(cmd)` from `root.go`
- Produces: a working `tengiz cleanup` command with flags `--all`, `--volumes`, `--dry-run`, `--yes/-y`, `--no-containers`, `--no-networks`, `--no-images`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	for _, flag := range []string{"all", "volumes", "dry-run", "yes", "no-containers", "no-networks", "no-images"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupNothingSelected(t *testing.T) {
	prev := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = prev }()

	rootCmd.SetArgs([]string{"cleanup", "--no-containers", "--no-networks", "--no-images"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when nothing selected")
	}
	if !strings.Contains(err.Error(), "nothing selected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCleanupDryRunDoesNotInvokeDocker(t *testing.T) {
	prev := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = prev }()

	fakeDir := t.TempDir()
	callLog := filepath.Join(fakeDir, "calls.log")
	script := "#!/bin/sh\necho \"CALL $*\" >> \"" + callLog + "\"\nexit 9\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "docker"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", fakeDir+string(os.PathListSeparator)+oldPath)
	defer os.Setenv("PATH", oldPath)

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--yes"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("cleanup --dry-run error: %v", err)
		}
	})

	if !strings.Contains(output, "containers") {
		t.Errorf("output missing containers step:\n%s", output)
	}
	if !strings.Contains(output, "images") {
		t.Errorf("output missing images step:\n%s", output)
	}

	calls, err := os.ReadFile(callLog)
	if err == nil && strings.TrimSpace(string(calls)) != "" {
		t.Errorf("dry-run invoked docker:\n%s", calls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `cleanup` command not registered

- [ ] **Step 3: Create the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=*) and tengiz-apps/* images
are always protected, so scale-to-zero cold starts and rollbacks keep
working.

Flags:
  --all            also prune all unused images (tengiz-apps/* still protected)
  --volumes        also prune unused volumes
  --no-containers  do not prune stopped containers
  --no-networks    do not prune unused networks
  --no-images      do not prune dangling images
  --dry-run        show what would be removed without removing anything
  --yes, -y        skip the confirmation prompt`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		yes, _ := cmd.Flags().GetBool("yes")
		noContainers, _ := cmd.Flags().GetBool("no-containers")
		noNetworks, _ := cmd.Flags().GetBool("no-networks")
		noImages, _ := cmd.Flags().GetBool("no-images")

		opts := runtime.CleanupOptions{
			All:        all,
			Volumes:    volumes,
			Containers: !noContainers,
			Networks:   !noNetworks,
			Images:     !noImages,
			DryRun:     dryRun,
		}

		if !opts.Containers && !opts.Images && !opts.Networks && !opts.Volumes && !opts.All {
			return fmt.Errorf("nothing selected: remove the --no-* flags or pass --volumes/--all")
		}

		if !dryRun && !yes && !confirmCleanup() {
			fmt.Println("[tengiz] cleanup cancelled")
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return err
		}

		if len(report.Steps) == 0 {
			fmt.Println("[tengiz] nothing to clean.")
			return nil
		}

		fmt.Printf("[tengiz] cleanup: %d steps\n", len(report.Steps))
		for _, step := range report.Steps {
			fmt.Printf("- %s: %s\n", step.Category, step.Command)
			if output := strings.TrimSpace(step.Output); output != "" {
				for _, line := range strings.Split(output, "\n") {
					fmt.Printf("  %s\n", line)
				}
			}
		}
		return nil
	},
}

func confirmCleanup() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return true
	}
	fmt.Print("Prune unused Docker resources? [y/N]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
```

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go` `init()`, after the existing `webhookCmd` flag registration (line 88), add:

```go
	cleanupCmd.Flags().Bool("all", false, "prune all unused images (not just dangling; tengiz-apps/* protected)")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("no-containers", false, "do not prune stopped containers")
	cleanupCmd.Flags().Bool("no-networks", false, "do not prune unused networks")
	cleanupCmd.Flags().Bool("no-images", false, "do not prune dangling images")
```

And before the closing of `init()`, after `rootCmd.AddCommand(notificationCmd)` (line 75), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (3 tests)

- [ ] **Step 6: Run the full suite once**

Run: `go test ./... -v -count=1`

Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Update documentation and mark the feature complete

**Files:**
- Modify: `README.md` — add `tengiz cleanup` CLI Reference section
- Modify: `AGENTS.md` — add the command to the CLI list
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 ✅ Implemented
- Test: none (documentation only); run verification commands

- [ ] **Step 1: Add CLI Reference section to README.md**

After the `### tengiz ps` section (ends at README.md line 150), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Also prune all unused images (not just dangling ones) |
| `--volumes` | Also prune unused volumes |
| `--dry-run` | Show what would be removed without removing anything |
| `--yes`, `-y` | Skip the confirmation prompt |
| `--no-containers` | Do not prune stopped containers |
| `--no-networks` | Do not prune unused networks |
| `--no-images` | Do not prune dangling images |

Primarily `docker image prune` (dangling), `docker container prune`, and `docker network prune`; `--volumes` adds `docker volume prune`, `--all` also removes non-dangling unused images. **Tengiz-managed containers (labeled `tengiz-app=<name>`) and `tengiz-apps/*` images are always protected** — stopped scale-to-zero containers and rollback images survive. When stdin is not a TTY, the confirmation prompt is skipped automatically; in CI pass `--yes`.
```

- [ ] **Step 2: Add the command to AGENTS.md CLI list**

After the `tengiz ps` line in the `## CLI` block, add:

```
tengiz cleanup [--all] [--volumes] [--dry-run] [--yes] → prune unused Docker resources (stopped Tengiz containers + tengiz-apps/* images always protected)
```

- [ ] **Step 3: Mark the feature implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, change row `| 6 | **Docker Housekeeping** ⬜ | ...` to `| 6 | **Docker Housekeeping** ✅ | ...` (keep IMPACT/EFFORT/ALIGNMENT values unchanged).

Add to the `✅ Implemented Features (Not Pending)` table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
```

- [ ] **Step 4: Full verification**

Run: `go build -o tengiz .`

Expected: builds

Run: `go vet ./...`

Expected: no issues

Run: `go test ./... -v -count=1`

Expected: all pass

Manually smoke-test (requires Docker): `tengiz cleanup --dry-run` prints the planned steps and invokes no destructive command.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 "Docker Housekeeping" (P0): label-based `docker system prune`, `tengiz cleanup` command.
- `tengiz cleanup` command ✅ (Task 3)
- Label-based protection so Tengiz-managed containers survive ✅ (constant `label!=tengiz-app` in every container prune; Task 1/2)
- Granular pruning of containers/images/networks/volumes ✅ (Task 1/2)
- `tengiz-apps/*` image protection for rollback integrity ✅ (Task 2 `selectImagePruneCandidates` test)
- Docs/README/AGENTS/FUTURES updated ✅ (Task 4)

- **Placeholder scan:** No TBD/TODO/"similar to Task N" — every step contains complete code and exact commands.

- **Type consistency:** `CleanupOptions` and `(*CleanupReport, error)` are identical in the interface (runtime.go), stub, dockerRuntime, all three mocks, and the CLI caller. `selectImagePruneCandidates(allLines, protectedLines string) []string` is defined and used once. `labelKey` reused from `docker.go:76` (verified present).

- **Cross-interface compile risk:** adding `Cleanup` to `Manager` breaks `mockRuntime` (idle/proxy tests) and `mockRTForDeploy` (cli root test); Task 2 Step 5 adds the method to all three, and Task 2 Step 7 runs `go build ./...` plus the full test suite to prove it.