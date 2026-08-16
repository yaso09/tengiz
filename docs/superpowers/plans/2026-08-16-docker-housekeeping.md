# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, networks, and volumes while guaranteeing that Tengiz-managed containers and images are never touched.

**Architecture:** A new `Cleanup(ctx, opts) (*CleanupReport, error)` method on `runtime.Manager` runs per-category `docker container/network/volume/image prune` commands behind the existing exec-based `dockerRuntime`. All Tengiz-managed containers carry the `tengiz-app=<name>` label and are protected with a `label!=tengiz-app` prune filter; images in the `tengiz-apps/*` namespace are never removed (dangling images and unused non-Tengiz images only), so the existing `KeepLastNImages` retention policy stays authoritative. A `cleanupOptions(cmd)` helper maps Cobra category flags to a `runtime.CleanupOptions` struct, defaulting to all categories when no flag is given.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- Container prune MUST use `--filter label!=tengiz-app` — Tengiz containers are always labeled `tengiz-app=<name>` (see `internal/runtime/docker.go:98`, `:125`, `:516`), so no Tengiz-managed container (running or stopped) is ever removed
- Images with the `tengiz-apps/*` repository prefix MUST never be removed by cleanup — retention of old Tengiz images is owned by `KeepLastNImages`
- Dangling images (`<none>:<none>`) are handled by `docker image prune -f` and skipped by the unused-image scan
- No new external Go dependencies (Docker CLI only)
- CLI category flags: `--containers`, `--images`, `--networks`, `--volumes`; when none are passed, ALL categories are cleaned
- Cleanup is global (all environments) — no `--env` flag; protection is label-based, so every env's containers are safe
- `countDeleted` targets the modern docker prune output format (one deleted item per line, docker ≥ 23); old single-line formats are counted coarsely (one item) but never crash
- Adding `Cleanup` to the `Manager` interface requires a no-op method on every mock implementing it: `stubManager`, `mockRTForDeploy`, and both `mockRuntime` types
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`/`CleanupReport`/`CleanupCategory` types, pure helpers (`buildPruneCommand`, `countDeleted`, `shouldPruneImage`), `dockerRuntime.Cleanup`, `pruneUnusedImages` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + no-op stub |
| `internal/runtime/cleanup_test.go` | Unit tests for helpers, stub test, fake-docker integration test |
| `internal/cli/root.go` | `cleanupCmd` cobra command, flag registration, `cleanupOptions(cmd)` + `formatCleanupReport(report)` helpers |
| `internal/cli/cleanup_test.go` | New file: command registration, flag presence, option-mapping, report formatting tests |
| `internal/cli/root_test.go`, `internal/idle/idle_test.go`, `internal/proxy/proxy_test.go` | Add no-op `Cleanup` to each mock `Manager` so the repo compiles |
| `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` | Document the command and mark feature #6 implemented |

---

### Task 1: Add the Cleanup API surface to the runtime package

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types + pure helpers (top of file)
- Modify: `internal/runtime/runtime.go:36` — add `Cleanup` to `Manager` interface; add stub method after line 119
- Modify: `internal/runtime/cleanup_test.go` — add helper + stub tests
- Modify: `internal/cli/root_test.go:100` — add no-op `Cleanup` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:34` — add no-op `Cleanup` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:35` — add no-op `Cleanup` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Networks, Volumes bool}`, `runtime.CleanupReport{ContainersRemoved, ImagesRemoved, DanglingImagesRemoved, NetworksRemoved, VolumesRemoved int}`, `runtime.CleanupCategory` constants `CleanupContainers/CleanupImages/CleanupNetworks/CleanupVolumes`, `runtime.Manager.Cleanup(ctx, opts) (*CleanupReport, error)`, `runtime.buildPruneCommand(category) []string`, `runtime.countDeleted(output string) int`, `runtime.shouldPruneImage(reference, imageID, usedContainers string) bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestCountDeleted(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{"empty output", "", 0},
		{"nothing deleted", "Total reclaimed space: 0B", 0},
		{
			"one container",
			"Deleted Containers:\nabc123abc123\n\nTotal reclaimed space: 0B",
			1,
		},
		{
			"two images with sha256 prefix",
			"Deleted Images:\nsha256:abc123abc123\nsha256:def456def456\n\nTotal reclaimed space: 1.2kB",
			2,
		},
		{
			"named volume",
			"Deleted Volumes:\nmyapp_data\n\nTotal reclaimed space: 0B",
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countDeleted(tt.output); got != tt.want {
				t.Errorf("countDeleted() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBuildPruneCommand(t *testing.T) {
	tests := []struct {
		category CleanupCategory
		want     []string
	}{
		{CleanupContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CleanupImages, []string{"image", "prune", "-f"}},
		{CleanupNetworks, []string{"network", "prune", "-f"}},
		{CleanupVolumes, []string{"volume", "prune", "-f"}},
	}
	for _, tt := range tests {
		got := buildPruneCommand(tt.category)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildPruneCommand(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestShouldPruneImage(t *testing.T) {
	tests := []struct {
		name           string
		reference      string
		imageID        string
		usedContainers string
		want           bool
	}{
		{"tengiz image protected", "tengiz-apps/myapp:v1", "img1", "", false},
		{"dangling image skipped", "<none>:<none>", "img2", "", false},
		{"image in use skipped", "busybox:latest", "img3", "c1\nc2\n", false},
		{"empty reference skipped", "", "img4", "", false},
		{"unused non-tengiz pruned", "busybox:latest", "img5", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPruneImage(tt.reference, tt.imageID, tt.usedContainers); got != tt.want {
				t.Errorf("shouldPruneImage(%q, %q, %q) = %v, want %v",
					tt.reference, tt.imageID, tt.usedContainers, got, tt.want)
			}
		})
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestCountDeleted|TestBuildPruneCommand|TestShouldPruneImage|TestStubCleanup" -v -count=1`

Expected: FAIL — `undefined: countDeleted`, `undefined: CleanupCategory`, `undefined: CleanupOptions` (nothing compiles yet)

- [ ] **Step 3: Add types and pure helpers to `internal/runtime/cleanup.go`**

Add at the top of the file, after the import block:

```go
type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupVolumes    CleanupCategory = "volumes"
)

type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
}

type CleanupReport struct {
	ContainersRemoved     int
	ImagesRemoved         int
	DanglingImagesRemoved int
	NetworksRemoved       int
	VolumesRemoved        int
}

func buildPruneCommand(category CleanupCategory) []string {
	switch category {
	case CleanupContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case CleanupImages:
		return []string{"image", "prune", "-f"}
	case CleanupNetworks:
		return []string{"network", "prune", "-f"}
	case CleanupVolumes:
		return []string{"volume", "prune", "-f"}
	}
	return nil
}

func countDeleted(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.Contains(line, " ") {
			continue // section header like "Deleted Containers:"
		}
		count++
	}
	return count
}

func shouldPruneImage(reference, imageID, usedContainers string) bool {
	if reference == "" || imageID == "" {
		return false
	}
	if strings.HasPrefix(reference, "tengiz-apps/") {
		return false
	}
	if strings.HasPrefix(reference, "<none>:") {
		return false
	}
	if strings.TrimSpace(usedContainers) != "" {
		return false
	}
	return true
}
```

The file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` — all helpers only need `strings`, which is present.

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface and stub in `internal/runtime/runtime.go`**

In the interface (after the `KeepLastNImages` line at `runtime.go:36`):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

In the stub (after the `KeepLastNImages` stub method at `runtime.go:119`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

- [ ] **Step 5: Add no-op `Cleanup` to the three mock Managers**

`internal/cli/root_test.go` — after the `KeepLastNImages` line (`root_test.go:99`):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

`internal/idle/idle_test.go` — after the `KeepLastNImages` line (`idle_test.go:33`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

`internal/proxy/proxy_test.go` — after the `KeepLastNImages` line (`proxy_test.go:34`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestCountDeleted|TestBuildPruneCommand|TestShouldPruneImage|TestStubCleanup" -v -count=1`

Expected: PASS (4 tests)

Run: `go build ./...`

Expected: Build succeeds (mocks now satisfy the extended `Manager` interface)

- [ ] **Step 7: Run runtime + affected package tests**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -count=1`

Expected: All PASS (proxy tests are slow ~2s each; idle tests are time-sensitive but pass)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Cleanup API to runtime.Manager with prune helpers"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` with a fake-docker integration test

**Files:**
- Modify: `internal/runtime/cleanup.go` — implement `dockerRuntime.Cleanup`, `runPrune`, `pruneUnusedImages`
- Modify: `internal/runtime/cleanup_test.go` — add `TestDockerCleanupRunsPruneCommands` integration test using a fake `docker` binary

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport`, `buildPruneCommand`, `countDeleted`, `shouldPruneImage`, `RemoveImage` (all from Task 1)
- Produces: working `dockerRuntime.Cleanup(ctx, opts) (*CleanupReport, error)` that runs real `docker` prune commands and reports counts

- [ ] **Step 1: Write the failing integration test**

```go
// internal/runtime/cleanup_test.go — add to the same file
func TestDockerCleanupRunsPruneCommands(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls.log")
	fakeDocker := filepath.Join(dir, "docker")

	script := `#!/bin/sh
echo "$@" >> "$LOG"
case "$1" in
  container)
    echo "Deleted Containers:"
    echo "abc123abc123"
    echo
    echo "Total reclaimed space: 0B"
    ;;
  network)
    echo "Deleted Networks:"
    echo "net1"
    echo
    echo "Total reclaimed space: 0B"
    ;;
  volume)
    echo "Deleted Volumes:"
    echo "vol1"
    echo
    echo "Total reclaimed space: 0B"
    ;;
  image)
    if [ "$2" = "ls" ]; then
      echo "tengiz-apps/myapp:v1|img1"
      echo "busybox:latest|img2"
      echo "<none>:<none>|img3"
    else
      echo "Deleted Images:"
      echo "img3"
      echo
      echo "Total reclaimed space: 0B"
    fi
    ;;
  ps)
    # busybox (img2) is unused: print nothing
    ;;
  rmi)
    ;;
esac
`
	script = strings.ReplaceAll(script, "$LOG", logFile)
	if err := os.WriteFile(fakeDocker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}

	report, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Networks: true, Volumes: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", report.ContainersRemoved)
	}
	if report.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", report.NetworksRemoved)
	}
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.DanglingImagesRemoved != 1 {
		t.Errorf("DanglingImagesRemoved = %d, want 1", report.DanglingImagesRemoved)
	}
	if report.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1 (busybox:latest)", report.ImagesRemoved)
	}

	logContent, _ := os.ReadFile(logFile)
	if !strings.Contains(string(logContent), "label!=tengiz-app") {
		t.Errorf("container prune missing label filter, calls:\n%s", logContent)
	}
	if strings.Contains(string(logContent), "rmi -f tengiz-apps") {
		t.Errorf("tengiz-managed image was removed:\n%s", logContent)
	}
}
```

Update the imports at the top of `cleanup_test.go` to:

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

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerCleanupRunsPruneCommands -v -count=1`

Expected: FAIL — `NewDocker()` returns ok but `dockerRuntime` has no `Cleanup` method, or the call is rejected by the fake binary with an empty report

- [ ] **Step 3: Implement `dockerRuntime.Cleanup`, `runPrune`, and `pruneUnusedImages` in `internal/runtime/cleanup.go`**

Add at the end of `cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{}

	if opts.Containers {
		n, err := r.runPrune(ctx, CleanupContainers)
		if err != nil {
			return nil, err
		}
		report.ContainersRemoved = n
	}
	if opts.Networks {
		n, err := r.runPrune(ctx, CleanupNetworks)
		if err != nil {
			return nil, err
		}
		report.NetworksRemoved = n
	}
	if opts.Volumes {
		n, err := r.runPrune(ctx, CleanupVolumes)
		if err != nil {
			return nil, err
		}
		report.VolumesRemoved = n
	}
	if opts.Images {
		n, err := r.runPrune(ctx, CleanupImages)
		if err != nil {
			return nil, err
		}
		report.DanglingImagesRemoved = n

		removed, err := r.pruneUnusedImages(ctx)
		if err != nil {
			return nil, err
		}
		report.ImagesRemoved = removed
	}

	return report, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, category CleanupCategory) (int, error) {
	args := buildPruneCommand(category)
	if len(args) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return countDeleted(string(out)), nil
}

func (r *dockerRuntime) pruneUnusedImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "ls",
		"--format", "{{.Repository}}:{{.Tag}}|{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image ls: %w", err)
	}

	removed := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		reference, id := parts[0], parts[1]

		usedCmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "ancestor="+id)
		usedOut, usedErr := usedCmd.CombinedOutput()
		if usedErr != nil {
			continue
		}
		if !shouldPruneImage(reference, id, string(usedOut)) {
			continue
		}
		if err := r.RemoveImage(ctx, reference); err != nil {
			log.Printf("[runtime] failed to remove unused image %s: %v", reference, err)
			continue
		}
		removed++
	}
	return removed, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestDockerCleanupRunsPruneCommands -v -count=1`

Expected: PASS — `ContainersRemoved=1`, `NetworksRemoved=1`, `VolumesRemoved=1`, `DanglingImagesRemoved=1`, `ImagesRemoved=1`; log contains `label!=tengiz-app` and no `rmi -f tengiz-apps`

- [ ] **Step 5: Run all runtime tests + vet**

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

Run: `go vet ./internal/runtime/...`

Expected: No issues

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with label-protected pruning"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()`, add command var + `cleanupOptions(cmd)` + `formatCleanupReport(report)` helpers
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()` (Task 1/2)
- Produces: `tengiz cleanup [--containers] [--images] [--networks] [--volumes]` command; helpers `cleanupOptions(cmd *cobra.Command) runtime.CleanupOptions` and `formatCleanupReport(report *runtime.CleanupReport) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go (new file)
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup-test"}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	return cmd
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandHasCategoryFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "networks", "volumes"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	opts := cleanupOptions(newCleanupTestCmd())
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes {
		t.Errorf("cleanupOptions() default = %+v, want all true", opts)
	}
}

func TestCleanupOptionsContainersOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.Flags().Set("containers", "true"); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptions(cmd)
	if !opts.Containers {
		t.Error("Containers should be true with --containers")
	}
	if opts.Images || opts.Networks || opts.Volumes {
		t.Errorf("only Containers should be true, got %+v", opts)
	}
}

func TestCleanupOptionsImagesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.Flags().Set("images", "true"); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptions(cmd)
	if !opts.Images {
		t.Error("Images should be true with --images")
	}
	if opts.Containers || opts.Networks || opts.Volumes {
		t.Errorf("only Images should be true, got %+v", opts)
	}
}

func TestFormatCleanupReport(t *testing.T) {
	report := &runtime.CleanupReport{
		ContainersRemoved: 2, ImagesRemoved: 3, DanglingImagesRemoved: 1,
		NetworksRemoved: 1, VolumesRemoved: 4,
	}
	got := formatCleanupReport(report)
	want := "[tengiz] cleanup complete: 2 containers, 3 images (1 dangling), 1 networks, 4 volumes removed\n"
	if got != want {
		t.Errorf("formatCleanupReport() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptions`, `undefined: formatCleanupReport`

- [ ] **Step 3: Register `cleanupCmd` and flags in `internal/cli/root.go` `init()`**

After `rootCmd.AddCommand(psCmd)` (`root.go:41`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

At the end of `init()` (after the webhook flags at `root.go:88`):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling and unused images not managed by Tengiz")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
```

- [ ] **Step 4: Add `cleanupCmd` and helpers to `internal/cli/root.go`**

Add the command right after the `psCmd` block (which ends at `root.go:601`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long:  "Prunes stopped containers, unused images, networks, and volumes. " +
		"Containers managed by Tengiz (labeled tengiz-app) and images in the tengiz-apps/ " +
		"namespace are never removed. By default all categories are cleaned; use flags to " +
		"select specific categories.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := cleanupOptions(cmd)
		report, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(formatCleanupReport(report))
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	all := !containers && !images && !networks && !volumes
	return runtime.CleanupOptions{
		Containers: all || containers,
		Images:     all || images,
		Networks:   all || networks,
		Volumes:    all || volumes,
	}
}

func formatCleanupReport(report *runtime.CleanupReport) string {
	return fmt.Sprintf("[tengiz] cleanup complete: %d containers, %d images (%d dangling), %d networks, %d volumes removed\n",
		report.ContainersRemoved, report.ImagesRemoved, report.DanglingImagesRemoved,
		report.NetworksRemoved, report.VolumesRemoved)
}
```

All required imports (`context`, `fmt`, `runtime`, `cobra`) are already present in `root.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (6 tests)

- [ ] **Step 6: Build + vet**

Run: `go build ./...`

Expected: Build succeeds

Run: `go vet ./internal/cli/...`

Expected: No issues

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation and feature tracking

**Files:**
- Modify: `README.md` — add a `tengiz cleanup` section after the `tengiz ps` section (`README.md:150`)
- Modify: `AGENTS.md:43-46` — add `cleanup` to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as Implemented (row at line 19 + Implemented table + description section)

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `tengiz ps` section (after line 150, before `### tengiz logs`):

```markdown
### `tengiz cleanup [--containers] [--images] [--networks] [--volumes]`

Prune unused Docker resources to reclaim disk space. Continuous deploys and scale-to-zero leave behind stopped containers, dangling images, unused networks, and orphaned volumes. Containers managed by Tengiz (labeled `tengiz-app`) and images built by Tengiz (`tengiz-apps/*`) are always protected and never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Remove dangling images and unused images not built by Tengiz |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes |

With no flags, all four categories are cleaned:

```
tengiz cleanup
tengiz cleanup --images --volumes
```

Output shows how many of each resource was reclaimed:

```
[tengiz] cleanup complete: 3 containers, 2 images (1 dangling), 1 networks, 4 volumes removed
```
```

- [ ] **Step 2: Update `AGENTS.md` command list**

After the `tengiz ps` line (`AGENTS.md:43`):

```
tengiz cleanup          → prune unused Docker containers/images/networks/volumes (tengiz-managed containers protected)
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

Change the P0 row (line 19) from `⬜` to `✅`:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "Implemented Features" table (after line 253, the Webhook row):

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-16) |
```

Add a status line to the "Docker Housekeeping (Otomatik Temizlik)" description section (after its `- **Detected:** 2026-07-14` line at line 381):

```
- **Status:** ✅ Implemented (2026-08-16)
```

- [ ] **Step 4: Verify docs render and commit**

Run: `git diff --stat`

Expected: README.md, AGENTS.md, docs/FUTURES_FEATURES.md all modified

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

### Task 5: Full verification and self-review

**Files:**
- No new files — runs the full suite and reviews the spec

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS (proxy TCP-dial tests are slow ~2s each; idle tests are time-sensitive with 50ms granularity)

- [ ] **Step 2: Run vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Self-review against the feature spec**

Check each requirement from `docs/FUTURES_FEATURES.md` feature #6:

- `tengiz cleanup` command ✅ (Task 3)
- Periodic cleanup of unused volumes/networks/containers/images ✅ (Task 2 — per-category prune; a periodic/daemon mode is documented as future work below)
- Label-based filtering so Tengiz-managed containers are protected ✅ (Task 1 — `label!=tengiz-app` filter; Task 2 integration test asserts the filter is passed and `tengiz-apps/*` images are never removed)
- Cleanup helper containers (build containers) ✅ (Task 2 — `docker container prune` with `label!=tengiz-app` removes any non-Tengiz stopped container, including helper/build containers)

Out of scope (noted for a future plan, not a gap): periodic/scheduled cleanup daemon, `--cache`/`--gc` build-cache and git-gc pruning (feature #103), and stale-container detection (feature #47).

- [ ] **Step 4: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task" — none present. Every code step contains complete, copyable code.

- [ ] **Step 5: Type consistency check**

- `runtime.CleanupOptions{Containers, Images, Networks, Volumes bool}` — defined Task 1, used Tasks 2, 3
- `runtime.CleanupReport{ContainersRemoved, ImagesRemoved, DanglingImagesRemoved, NetworksRemoved, VolumesRemoved int}` — defined Task 1, populated Task 2, formatted Task 3
- `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — added Task 1, all four mock types updated in Task 1 Step 5
- `cleanupOptions(cmd *cobra.Command) runtime.CleanupOptions` — defined and tested Task 3, used by `cleanupCmd.RunE`
- `formatCleanupReport(report *runtime.CleanupReport) string` — defined and tested Task 3
- `buildPruneCommand(category CleanupCategory) []string` — `CleanupContainers` always includes `label!=tengiz-app`; used by `runPrune` Task 2

- [ ] **Step 6: Final commit (if any step produced uncommitted changes)**

```bash
git status
git add -A
git commit -m "test: final verification for docker housekeeping"
```

Only run this if `git status` shows uncommitted changes; otherwise skip.