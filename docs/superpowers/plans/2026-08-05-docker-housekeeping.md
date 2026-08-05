# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` — a label-protected Docker housekeeping command that prunes stopped containers, dangling images, unused networks, and build cache (with opt-in `--images` and `--volumes` flags) and reports disk usage reclaimed.

**Architecture:** Extend the `runtime.Manager` interface with `Prune(ctx, PruneOptions)` and `SystemDF(ctx)` methods, implemented by the existing exec-based `dockerRuntime` using `docker system prune` and `docker system df`. The CLI wires a new `cleanup` cobra command with a confirmation prompt and a before/after disk-usage report. A reference whitelist preserves `tengiz-apps/*` images so the existing rollback feature (`CreateFromImage`) keeps working. Pure arg-builders and image-selection helpers are kept as standalone functions so they are unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK).

## Global Constraints

- Go 1.26, module `github.com/yaso09/tengiz`. No new third-party dependencies — `go.mod` stays unchanged.
- All Docker access via `os/exec` calling the `docker` CLI (existing pattern; see `internal/runtime/docker.go`).
- Protect Tengiz-managed containers with the label filter `label!=tengiz-app` on `docker system prune`. Every Tengiz container is created with `--label tengiz-app=<name>` (see `docker.go` `Create`, `CreateFromImage`, `CreateVersioned`), so this filter prunes only non-Tengiz stopped containers.
- Preserve `tengiz-apps/*` images (built in `internal/builder/builder.go`) so rollback keeps working. `docker image prune -a` would delete them (they are tagged but unreferenced after the old container is removed), so the `--images` path removes unused tagged images via a whitelist, never via `image prune -a`.
- Tests must NOT require a running Docker daemon. Use `runtime.NewStub()`, pure functions, and injected `io.Reader`s.
- Work on a feature branch `feat/cleanup` (repo rule: "Yeni özellik geliştirirken branch oluştur").
- Before every commit run: `go test ./... -v -count=1`, `go vet ./...`, and `go build -o tengiz .` must all pass.
- Commit after each task (repo rule: "Her değişiklikte test ekle/güncelle, testleri geçir, sonra commit et"), message prefixed `feat:`.

---

### Task 1: Extend `runtime.Manager` with `Prune` and `SystemDF`

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface) and `internal/runtime/runtime.go:113-122` (stub)
- Test: `internal/runtime/cleanup_test.go`
- Modify: `internal/cli/root_test.go:76-100` (`mockRTForDeploy`)
- Modify: `internal/proxy/proxy_test.go:19-35` (`mockRuntime`)
- Modify: `internal/idle/idle_test.go:18-34` (`mockRuntime`)

**Interfaces:**
- Consumes: existing `Manager` interface in `internal/runtime/runtime.go`.
- Produces: `runtime.PruneOptions` struct `{ Images bool; Volumes bool }`; new `Manager` methods `Prune(ctx context.Context, opts PruneOptions) error` and `SystemDF(ctx context.Context) (string, error)`. `PruneOptions` is defined here so later tasks and the CLI can reference it. All test mocks that satisfy `Manager` must implement the two new methods (compile requirement).

- [ ] **Step 1: Create the feature branch**

Run: `git checkout -b feat/cleanup`

- [ ] **Step 2: Write the failing tests**

Add to `internal/runtime/cleanup_test.go` (after the existing `TestStubKeepLastNImages`):

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	if err := m.Prune(context.Background(), PruneOptions{Images: true, Volumes: true}); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty", out)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestStubPrune|TestStubSystemDF' -v -count=1`
Expected: FAIL — compile error: `m.Prune undefined` / `m.SystemDF undefined` (and `PruneOptions undefined`).

- [ ] **Step 4: Add `PruneOptions` and the new methods to the interface**

Edit `internal/runtime/runtime.go`. Add the struct above the interface (before `type Manager interface`):

```go
type PruneOptions struct {
	Images  bool
	Volumes bool
}
```

Add to the `Manager` interface, right after the `KeepLastNImages` line:

```go
	Prune(ctx context.Context, opts PruneOptions) error
	SystemDF(ctx context.Context) (string, error)
```

- [ ] **Step 5: Add stub implementations**

In `internal/runtime/runtime.go`, add to `stubManager` after the `KeepLastNImages` method:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) error {
	return nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 6: Update the three test mocks that implement `Manager`**

These mocks fail to compile once the interface grows. Add the same two methods to each:

`internal/cli/root_test.go` — after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

`internal/proxy/proxy_test.go` — after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

`internal/idle/idle_test.go` — after the `KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

If any of these files does not already import `context`, add it to the import block.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./... -v -count=1`
Expected: PASS — including `TestStubPrune`, `TestStubSystemDF`, and all existing tests that rely on the mocks.

- [ ] **Step 8: Vet, build, commit**

Run: `go vet ./... && go build -o tengiz .`

Run:
```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Prune and SystemDF to runtime.Manager interface"
```

---

### Task 2: Implement `dockerRuntime.SystemDF` and label-protected `Prune`

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`
- Consumes from Task 1: `PruneOptions`, `Manager.Prune`, `Manager.SystemDF`.

**Interfaces:**
- Consumes: `PruneOptions` from Task 1.
- Produces (used by Task 3): `dockerRuntime.Prune` runs the label-protected `docker system prune`; `SystemDF` returns raw `docker system df` output. Produces standalone testable builders:
  - `systemPruneArgs(opts PruneOptions) []string` → `["system", "prune", "-f", "--filter", "label!=tengiz-app"]` (+ `"--volumes"` when `opts.Volumes`).
  - `systemDFArgs() []string` → `["system", "df"]`.

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestSystemPruneArgsDefault(t *testing.T) {
	got := systemPruneArgs(PruneOptions{})
	want := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("systemPruneArgs() = %v, want %v", got, want)
	}
}

func TestSystemPruneArgsWithVolumes(t *testing.T) {
	got := systemPruneArgs(PruneOptions{Volumes: true})
	want := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("systemPruneArgs() = %v, want %v", got, want)
	}
}

func TestSystemDFArgs(t *testing.T) {
	got := systemDFArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("systemDFArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestSystemPruneArgs|TestSystemDFArgs' -v -count=1`
Expected: FAIL — compile error: `undefined: systemPruneArgs` / `undefined: systemDFArgs`.

- [ ] **Step 3: Implement the minimal code**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
)

func systemPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func systemDFArgs() []string {
	return []string{"system", "df"}
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) error {
	cmd := exec.CommandContext(ctx, "docker", systemPruneArgs(opts)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestSystemPruneArgs|TestSystemDFArgs' -v -count=1`
Expected: PASS.

- [ ] **Step 5: Full suite, vet, build, commit**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`

Run:
```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add label-protected docker system prune and system df to runtime"
```

---

### Task 3: Preserve `tengiz-apps/*` images under `--images`

**Files:**
- Modify: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`
- Consumes from Task 2: `dockerRuntime.Prune`, `dockerRuntime.SystemDF`, `dockerRuntime.RemoveImage` (existing).

**Interfaces:**
- Consumes: `PruneOptions` from Task 1; `RemoveImage(ctx, imageTag)` (existing, `internal/runtime/cleanup.go:12`).
- Produces (used by Task 4 CLI): `Prune` behavior — when `opts.Images` is true, after the system prune, remove every unused tagged image EXCEPT those whose ID belongs to `tengiz-apps/*`. Standalone testable helpers:
  - `tengizImagesArgs() []string` → `["images", "--filter", "reference=tengiz-apps/*", "--format", "{{.ID}}"]`
  - `allImagesArgs() []string` → `["images", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"]`
  - `type imageLine struct { ID, FullName string }`
  - `parseImageLines(out string) []imageLine`
  - `selectImagesToPrune(lines []imageLine, protected map[string]bool) []imageLine`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/prune_test.go`:

```go
func TestTengizImagesArgs(t *testing.T) {
	got := tengizImagesArgs()
	want := []string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.ID}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tengizImagesArgs() = %v, want %v", got, want)
	}
}

func TestAllImagesArgs(t *testing.T) {
	got := allImagesArgs()
	want := []string{"images", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("allImagesArgs() = %v, want %v", got, want)
	}
}

func TestParseImageLines(t *testing.T) {
	out := "abc123def456 tengiz-apps/myapp:production-abc\ndef456abc123 nginx:alpine\n<none> <none>:<none>\n\n"
	lines := parseImageLines(out)
	if len(lines) != 3 {
		t.Fatalf("parseImageLines() = %d lines, want 3", len(lines))
	}
	if lines[0].ID != "abc123def456" || lines[0].FullName != "tengiz-apps/myapp:production-abc" {
		t.Errorf("line[0] = %+v", lines[0])
	}
	if lines[1].FullName != "nginx:alpine" {
		t.Errorf("line[1] = %+v", lines[1])
	}
	if lines[2].ID != "<none>" {
		t.Errorf("line[2] = %+v", lines[2])
	}
}

func TestSelectImagesToPrune(t *testing.T) {
	lines := []imageLine{
		{ID: "aaa", FullName: "tengiz-apps/myapp:production-abc"},
		{ID: "bbb", FullName: "nginx:alpine"},
		{ID: "ccc", FullName: "<none>:<none>"},
		{ID: "ddd", FullName: "tengiz-apps/otherapp:staging-xyz"},
		{ID: "eee", FullName: "redis:7"},
	}
	protected := map[string]bool{"aaa": true, "ddd": true}
	got := selectImagesToPrune(lines, protected)
	if len(got) != 2 {
		t.Fatalf("selectImagesToPrune() = %d items, want 2: %+v", len(got), got)
	}
	if got[0].ID != "bbb" || got[1].ID != "eee" {
		t.Errorf("selectImagesToPrune() = %+v, want [bbb eee]", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestTengizImagesArgs|TestAllImagesArgs|TestParseImageLines|TestSelectImagesToPrune' -v -count=1`
Expected: FAIL — compile error: `undefined: tengizImagesArgs` (and the other helpers).

- [ ] **Step 3: Implement the minimal code**

Edit `internal/runtime/prune.go` — add `strings` and `log` to the imports and append:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

```go
func tengizImagesArgs() []string {
	return []string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.ID}}"}
}

func allImagesArgs() []string {
	return []string{"images", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"}
}

type imageLine struct {
	ID       string
	FullName string
}

func parseImageLines(out string) []imageLine {
	var lines []imageLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		lines = append(lines, imageLine{ID: fields[0], FullName: fields[1]})
	}
	return lines
}

// selectImagesToPrune returns tagged images that are not Tengiz app images.
// Dangling (<none>:<none>) images are skipped: docker system prune removes them.
func selectImagesToPrune(lines []imageLine, protected map[string]bool) []imageLine {
	var toPrune []imageLine
	for _, line := range lines {
		if protected[line.ID] {
			continue
		}
		if line.FullName == "<none>:<none>" {
			continue
		}
		toPrune = append(toPrune, line)
	}
	return toPrune
}

func (r *dockerRuntime) tengizImageIDs(ctx context.Context) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "docker", tengizImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	ids := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids[id] = true
		}
	}
	return ids, nil
}

func (r *dockerRuntime) pruneUnprotectedImages(ctx context.Context) error {
	protected, err := r.tengizImageIDs(ctx)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", allImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	for _, img := range selectImagesToPrune(parseImageLines(string(out)), protected) {
		if err := r.RemoveImage(ctx, img.ID); err != nil {
			log.Printf("[tengiz] skip image %s: %v", img.ID, err)
		}
	}
	return nil
}
```

Change `Prune` to call `pruneUnprotectedImages` when `opts.Images` is set:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) error {
	cmd := exec.CommandContext(ctx, "docker", systemPruneArgs(opts)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	if opts.Images {
		return r.pruneUnprotectedImages(ctx)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestTengizImagesArgs|TestAllImagesArgs|TestParseImageLines|TestSelectImagesToPrune' -v -count=1`
Expected: PASS.

- [ ] **Step 5: Full suite, vet, build, commit**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`

Run:
```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: preserve tengiz-apps images when pruning unused images"
```

---

### Task 4: Add the `cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38-89` (register command + flags)
- Test helper reused: `captureOutput` in `internal/cli/root_test.go:57` (same package).

**Interfaces:**
- Consumes from Tasks 1–3: `runtime.Manager` with `Prune(ctx, PruneOptions) error` and `SystemDF(ctx) (string, error)`; `runtime.PruneOptions{Images, Volumes}`.
- Produces:
  - `cleanupCmd` (`*cobra.Command`, `Use: "cleanup"`), registered on `rootCmd` with flags `--force/-f`, `--images`, `--volumes`.
  - `type cleanupOptions struct { Force bool; Images bool; Volumes bool }`
  - `runCleanup(ctx context.Context, rt runtime.Manager, opts cleanupOptions, stdin io.Reader) error` — testable entry point.
  - `promptYesNo(prompt string, r io.Reader) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
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

func TestCleanupCommandFlags(t *testing.T) {
	for _, f := range []string{"force", "images", "volumes"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func TestPromptYesNo(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"y lowercase", "y\n", true},
		{"yes full word", "yes\n", true},
		{"Y uppercase", "Y\n", true},
		{"n lowercase", "n\n", false},
		{"no full word", "no\n", false},
		{"empty", "\n", false},
		{"garbage", "maybe\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promptYesNo("Continue? ", strings.NewReader(tt.input))
			if got != tt.expected {
				t.Errorf("promptYesNo(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRunCleanupForceSkipsPrompt(t *testing.T) {
	rt := runtime.NewStub()
	out := captureOutput(func() {
		runCleanup(context.Background(), rt, cleanupOptions{Force: true}, strings.NewReader("n\n"))
	})
	if !strings.Contains(out, "Disk usage before cleanup:") {
		t.Errorf("output missing before section: %q", out)
	}
	if !strings.Contains(out, "Disk usage after cleanup:") {
		t.Errorf("output missing after section: %q", out)
	}
}

func TestRunCleanupPromptNoCancels(t *testing.T) {
	rt := runtime.NewStub()
	out := captureOutput(func() {
		runCleanup(context.Background(), rt, cleanupOptions{}, strings.NewReader("n\n"))
	})
	if !strings.Contains(out, "Cleanup cancelled.") {
		t.Errorf("expected cancellation message, got %q", out)
	}
}

func TestRunCleanupPromptYesProceeds(t *testing.T) {
	rt := runtime.NewStub()
	out := captureOutput(func() {
		runCleanup(context.Background(), rt, cleanupOptions{}, strings.NewReader("y\n"))
	})
	if !strings.Contains(out, "Disk usage after cleanup:") {
		t.Errorf("expected cleanup to proceed, got %q", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestPromptYesNo|TestRunCleanup' -v -count=1`
Expected: FAIL — compile error: `undefined: cleanupCmd` / `undefined: promptYesNo` / `undefined: runCleanup`.

- [ ] **Step 3: Implement the minimal code**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources: stopped containers, dangling images, unused
networks and build cache.

Tengiz-managed containers (labeled tengiz-app=...) and Tengiz app images
(tengiz-apps/*) are always preserved so running apps and rollback keep working.

Use --images to also remove unused tagged images, and --volumes to also remove
unused volumes (volume data cannot be recovered).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := cleanupOptions{}
		opts.Force, _ = cmd.Flags().GetBool("force")
		opts.Images, _ = cmd.Flags().GetBool("images")
		opts.Volumes, _ = cmd.Flags().GetBool("volumes")
		return runCleanup(cmd.Context(), rt, opts, os.Stdin)
	},
}

type cleanupOptions struct {
	Force   bool
	Images  bool
	Volumes bool
}

func runCleanup(ctx context.Context, rt runtime.Manager, opts cleanupOptions, stdin io.Reader) error {
	if !opts.Force && !promptYesNo("Remove unused Docker resources? [y/N] ", stdin) {
		fmt.Println("Cleanup cancelled.")
		return nil
	}

	before, err := rt.SystemDF(ctx)
	if err != nil {
		return fmt.Errorf("disk usage (before): %w", err)
	}

	if err := rt.Prune(ctx, runtime.PruneOptions{Images: opts.Images, Volumes: opts.Volumes}); err != nil {
		return err
	}

	after, err := rt.SystemDF(ctx)
	if err != nil {
		return fmt.Errorf("disk usage (after): %w", err)
	}

	fmt.Println("Disk usage before cleanup:")
	fmt.Print(before)
	fmt.Println("Disk usage after cleanup:")
	fmt.Print(after)
	return nil
}

func promptYesNo(prompt string, r io.Reader) bool {
	fmt.Print(prompt)
	var answer string
	if _, err := fmt.Fscanln(r, &answer); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
```

Register the command and flags in `internal/cli/root.go` `init()`. Add `rootCmd.AddCommand(cleanupCmd)` near the other `rootCmd.AddCommand(...)` calls (e.g. right after `rootCmd.AddCommand(volumeCmd)` at line 64), and add the flag definitions at the end of `init()`:

```go
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("images", false, "also remove unused tagged images (tengiz-apps/* images are kept)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (volume data cannot be recovered)")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestPromptYesNo|TestRunCleanup' -v -count=1`
Expected: PASS.

- [ ] **Step 5: Full suite, vet, build, commit**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`

Run:
```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md:12-23` (Features list) and `README.md` CLI Reference (insert `tengiz cleanup` section after the `tengiz ps` section, ~line 150)
- Modify: `docs/FUTURES_FEATURES.md:19` (P0 row #6) and the "Docker Housekeeping" section (~line 377)

**Interfaces:**
- Consumes: the shipped CLI surface from Task 4 (`tengiz cleanup [--force] [--images] [--volumes]`).
- Produces: nothing consumed by code — documentation only.

- [ ] **Step 1: Add the feature bullet to the README**

In `README.md`, in the Features list (after the "Deployment history" bullet, line 20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space from stopped containers, dangling images, unused networks, and build cache without touching Tengiz apps.
```

- [ ] **Step 2: Add the CLI reference section**

In `README.md`, insert after the `### tengiz ps` section (after line 150, before `### tengiz logs`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources (stopped containers, dangling images, unused networks, build cache) to reclaim disk space. Tengiz-managed containers and `tengiz-apps/*` images are always preserved.

| Flag | Description |
|------|-------------|
| `-f`, `--force` | Skip the confirmation prompt |
| `--images` | Also remove unused tagged images (Tengiz app images are kept) |
| `--volumes` | Also remove unused volumes (volume data cannot be recovered) |

Without `--force`, the command asks for confirmation and then prints disk usage before and after cleanup.
```

- [ ] **Step 3: Mark the feature implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, row #6 in the P0 table (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. ✅ Implemented (2026-08-05) |
```

In the "## Docker Housekeeping (Otomatik Temizlik)" section (~line 377), add a status line after the description block:

```markdown
- **Status:** ✅ Implemented (2026-08-05)
```

- [ ] **Step 4: Verify the full project still passes**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`
Expected: PASS.

- [ ] **Step 5: Commit**

Run:
```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage.** The feature spec (#6 Docker Housekeeping) calls for: label-based `docker system prune` (Task 2, `--filter "label!=tengiz-app"`), a `tengiz cleanup` command (Task 4), protection of Tengiz-managed containers (Task 2 label filter), and periodic/reporting value — reporting is delivered as before/after `docker system df` output (Tasks 2 & 4). The "Why add" note about unreferenced image bloat is covered by the `--images` whitelist path (Task 3). Related #56 (per-category prune) is deliberately out of scope for this low-effort P0; not claimed.

**2. Placeholder scan.** Every step contains concrete code or exact commands with expected output. No "TBD", "handle errors", or "similar to" references. Mock updates in Task 1 spell out all three files.

**3. Type consistency.** `PruneOptions{Images, Volumes bool}` is defined once in Task 1 and used identically in Tasks 2–4. Method names `Prune`/`SystemDF` match across interface (Task 1), stub (Task 1), docker implementation (Tasks 2–3), CLI call sites (Task 4), and all mocks (Task 1). Helper names `systemPruneArgs`, `systemDFArgs`, `tengizImagesArgs`, `allImagesArgs`, `parseImageLines`, `selectImagesToPrune` are consistent between their definitions and tests. `cleanupOptions{Force, Images, Volumes}` is defined in Task 4 and used only there. `promptYesNo(prompt string, r io.Reader) bool` matches its tests.

**Execution handoff note:** Implement on branch `feat/cleanup`. Each task leaves the tree compiling and the full test suite green.
