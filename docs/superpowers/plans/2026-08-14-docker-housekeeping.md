# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, and networks — while never touching objects managed by Tengiz (all apps, previews, and deployments stay untouched).

**Architecture:** A new `Cleanup` + `DiskUsage` method pair on the `runtime.Manager` interface. `dockerRuntime` implements them via `docker` CLI subprocesses (`docker ps -aq`, `docker images -q`, `docker volume ls -q`, `docker network ls -q` for preview; matching `docker * prune -f` commands for removal). Tengiz safety is enforced with `label!=tengiz-app` / `label!=tengiz-env` filters so labeled objects are excluded from every prune. Image cleanup only targets dangling (`<none>`) images — the existing `KeepLastNImages` already bounds per-app versioned images. The CLI shows `docker system df` before/after, previews counts, asks for confirmation (skippable via `--yes`, previewable via `--dry-run`).

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` docker CLI (no Docker SDK, matching existing `runtime` package), existing `runtime.Manager` / `runtime.NewStub()` / `runtime.NewDocker()` patterns.

## Global Constraints

- Module `github.com/yaso09/tengiz`, Go 1.26. No new external dependencies.
- Tengiz-managed objects carry the `tengiz-app` and `tengiz-env` Docker labels (`internal/runtime/docker.go:76-77`). They must NEVER be pruned — every container prune MUST include `--filter label!=tengiz-app` and `--filter label!=tengiz-env`.
- Image cleanup removes ONLY dangling images (`--filter dangling=true`). Versioned per-app images (`tengiz-apps/<app>:<env>-<deploymentID>`) are bounded by the existing `runtime.Manager.KeepLastNImages` and must not be touched by `cleanup`.
- `tengiz cleanup` with no category flags enables all four categories (containers, images, volumes, networks).
- The default (non-`--dry-run`, non-`--yes`) flow MUST prompt for confirmation before removing anything.
- Build-cache pruning is OUT OF SCOPE — tracked separately as feature #103 (`tengiz cleanup --cache --gc`).
- `internal/cli/root_test.go` `mockRTForDeploy` MUST gain the two new `Manager` methods in the same task that extends the interface, or the module won't compile.
- Existing tests must continue to pass without modification.
- Command naming/copy follows the repo: help strings use "Tengiz", flags use kebab-case.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` (modify) | `CleanupOptions`, `CleanupResult` types; `*PruneArgs()` / `*ListArgs()` docker command builders; `countLines`; `dockerRuntime.Cleanup` + `DiskUsage` implementations |
| `internal/runtime/runtime.go` (modify) | Add `Cleanup` + `DiskUsage` to the `Manager` interface and to `stubManager` |
| `internal/runtime/cleanup_test.go` (modify) | Tests for stub Cleanup/DiskUsage, `countLines`, and all command builders |
| `internal/cli/root_test.go` (modify) | Add `Cleanup` + `DiskUsage` to `mockRTForDeploy` (keeps compilation green) |
| `internal/cli/cmd_cleanup.go` (create) | `cleanupCmd` + `cleanupOptionsFromFlags` + `runCleanupCommand` + `printCleanupResult` + `confirm` |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` and its flags in `init()` |
| `internal/cli/cmd_cleanup_test.go` (create) | CLI tests: registration, flags, option parsing, dry-run/yes/cancel flows, confirm, output formatting |
| `README.md` (modify) | New `tengiz cleanup` section in CLI Reference + Features bullet |
| `AGENTS.md` (modify) | Add `tengiz cleanup` line to the CLI list |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Cleanup types + docker command builders

**Files:**
- Modify: `internal/runtime/cleanup.go` (append to end of file)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (pure functions, no interface change)
- Produces:
  - `type CleanupOptions struct { DryRun, Containers, Images, Volumes, Networks bool }`
  - `type CleanupResult struct { ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int }`
  - `func containerPruneArgs() []string`, `func containerListArgs() []string`
  - `func imagePruneArgs() []string`, `func imageListArgs() []string`
  - `func volumePruneArgs() []string`, `func volumeListArgs() []string`
  - `func networkPruneArgs() []string`, `func networkListArgs() []string`
  - `func countLines(out []byte) int`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"whitespace only", "  \n  \n", 0},
		{"single id", "abc123\n", 1},
		{"three ids", "abc\ndef\nghi\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines([]byte(tt.in)); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanupPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "container prune",
			got:  containerPruneArgs(),
			want: []string{"container", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{
			name: "container list",
			got:  containerListArgs(),
			want: []string{"ps", "-aq",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{
			name: "image prune",
			got:  imagePruneArgs(),
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "image list",
			got:  imageListArgs(),
			want: []string{"images", "-q", "--filter", "dangling=true"},
		},
		{
			name: "volume prune",
			got:  volumePruneArgs(),
			want: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "volume list",
			got:  volumeListArgs(),
			want: []string{"volume", "ls", "-q", "--filter", "label!=tengiz-app"},
		},
		{
			name: "network prune",
			got:  networkPruneArgs(),
			want: []string{"network", "prune", "-f"},
		},
		{
			name: "network list",
			got:  networkListArgs(),
			want: []string{"network", "ls", "-q", "--filter", "dangling=true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %v", len(tt.got), len(tt.want), tt.got)
			}
			for i := range tt.got {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestCountLines|TestCleanupPruneArgs" -v -count=1`

Expected: FAIL with `undefined: countLines`, `undefined: containerPruneArgs` (etc.)

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages`):

```go
// CleanupOptions configures which Docker resource categories are cleaned.
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

// CleanupResult reports how many objects were removed per category.
type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}

// Tengiz-managed objects carry the tengiz-app / tengiz-env labels. The
// label!= filters exclude them so cleanup never touches deployed apps.
func containerPruneArgs() []string {
	return []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func containerListArgs() []string {
	return []string{
		"ps", "-aq",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func imageListArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func volumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "label!=tengiz-app"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

// countLines counts non-empty lines. Docker list commands print one object ID
// per line, so this tallies how many objects a category contains.
func countLines(out []byte) int {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestCountLines|TestCleanupPruneArgs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup types and docker command builders"
```

---

### Task 2: Extend Manager interface + dockerRuntime implementation

**Files:**
- Modify: `internal/runtime/runtime.go` (Manager interface + stub)
- Modify: `internal/runtime/cleanup.go` (dockerRuntime `Cleanup` + `DiskUsage`)
- Modify: `internal/cli/root_test.go` (add methods to `mockRTForDeploy`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, and all `*PruneArgs()`/`*ListArgs()` builders + `countLines` from Task 1
- Produces:
  - `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`
  - `runtime.Manager.DiskUsage(ctx context.Context) (string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.NetworksRemoved != 0 {
		t.Errorf("stub Cleanup() should return zero result, got %+v", res)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	out, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if out != "" {
		t.Errorf("stub DiskUsage() = %q, want empty string", out)
	}
}

func TestStubSatisfiesCleanupInterface(t *testing.T) {
	m := NewStub()
	var _ interface {
		Cleanup(context.Context, CleanupOptions) (CleanupResult, error)
		DiskUsage(context.Context) (string, error)
	} = m
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestStubDiskUsage|TestStubSatisfiesCleanupInterface" -v -count=1`

Expected: FAIL — `stubManager` does not implement `Cleanup`/`DiskUsage`

- [ ] **Step 3: Add methods to the Manager interface in `internal/runtime/runtime.go`**

Add to the `Manager` interface (after the `KeepLastNImages` line):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
	DiskUsage(ctx context.Context) (string, error)
```

- [ ] **Step 4: Add the stub implementations in `internal/runtime/runtime.go`**

Add to `stubManager` (after its `KeepLastNImages` method):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Add `Cleanup` + `DiskUsage` to `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the mock's `KeepLastNImages` method:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}

func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 6: Implement `Cleanup` + `DiskUsage` on `dockerRuntime`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	if opts.Containers {
		n, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ContainersRemoved = n
	}
	if opts.Images {
		n, err := r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ImagesRemoved = n
	}
	if opts.Volumes {
		n, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.VolumesRemoved = n
	}
	if opts.Networks {
		n, err := r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.NetworksRemoved = n
	}
	return result, nil
}

// cleanupContainers counts matching stopped containers, then prunes them unless
// dryRun. Tengiz containers are excluded via the label!= filters.
func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", containerListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", containerPruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker container prune: %w", err)
	}
	return count, nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", imageListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", imagePruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker image prune: %w", err)
	}
	return count, nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", volumeListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", volumePruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker volume prune: %w", err)
	}
	return count, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", networkListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", networkPruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker network prune: %w", err)
	}
	return count, nil
}

// DiskUsage returns the raw `docker system df` output for disk reporting.
func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "system", "df").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w", err)
	}
	return string(out), nil
}
```

- [ ] **Step 7: Run the runtime tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 8: Build the whole module (checks `mockRTForDeploy` compiles against the new interface)**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup and DiskUsage to runtime.Manager"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cmd_cleanup.go`
- Modify: `internal/cli/root.go` (register command + flags in `init()`)
- Create: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (`Cleanup`, `DiskUsage`), `runtime.CleanupOptions`, `runtime.CleanupResult` from Tasks 1-2
- Produces:
  - `cleanupCmd *cobra.Command`
  - `func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`
  - `type cleanupRunner interface { Cleanup(context.Context, runtime.CleanupOptions) (runtime.CleanupResult, error); DiskUsage(context.Context) (string, error) }`
  - `func runCleanupCommand(ctx context.Context, rt cleanupRunner, opts runtime.CleanupOptions, yes bool, out io.Writer, in io.Reader) error`
  - `func printCleanupResult(out io.Writer, res runtime.CleanupResult, dryRun bool)`
  - `func confirm(in io.Reader, out io.Writer, prompt string) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "dry-run", "yes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func newTestCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestCleanupOptionsFromFlagsAllDefault(t *testing.T) {
	opts, err := cleanupOptionsFromFlags(newTestCleanupCmd())
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks {
		t.Errorf("no category flags should enable all categories, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupOptionsFromFlagsSpecific(t *testing.T) {
	cmd := newTestCleanupCmd()
	cmd.Flags().Set("containers", "true")
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Containers {
		t.Error("Containers should be true")
	}
	if opts.Images || opts.Volumes || opts.Networks {
		t.Errorf("only Containers should be enabled, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDryRun(t *testing.T) {
	cmd := newTestCleanupCmd()
	cmd.Flags().Set("dry-run", "true")
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true when --dry-run is set")
	}
}

type cleanupMock struct {
	cleanupCalls []runtime.CleanupOptions
	dfOutput     string
	dfCalls      int
}

func (m *cleanupMock) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.cleanupCalls = append(m.cleanupCalls, opts)
	return runtime.CleanupResult{
		ContainersRemoved: 3,
		ImagesRemoved:     12,
		VolumesRemoved:    1,
		NetworksRemoved:   0,
	}, nil
}

func (m *cleanupMock) DiskUsage(ctx context.Context) (string, error) {
	m.dfCalls++
	return m.dfOutput, nil
}

func TestRunCleanupCommandDryRun(t *testing.T) {
	m := &cleanupMock{dfOutput: "Images: 100MB\n"}
	var out bytes.Buffer
	opts := runtime.CleanupOptions{DryRun: true, Containers: true, Images: true, Volumes: true, Networks: true}

	if err := runCleanupCommand(context.Background(), m, opts, false, &out, strings.NewReader("y\n")); err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if len(m.cleanupCalls) != 1 || !m.cleanupCalls[0].DryRun {
		t.Fatalf("expected exactly one DryRun=true cleanup call, got %+v", m.cleanupCalls)
	}
	if m.dfCalls != 1 {
		t.Errorf("DiskUsage called %d times, want 1 (dry-run should not print after-df)", m.dfCalls)
	}
	got := out.String()
	if !strings.Contains(got, "Images: 100MB") {
		t.Errorf("output missing docker system df, got:\n%s", got)
	}
	if !strings.Contains(got, "3 would be removed") {
		t.Errorf("output missing dry-run preview counts, got:\n%s", got)
	}
	if strings.Contains(got, "Proceed with cleanup") {
		t.Errorf("dry-run should not prompt for confirmation, got:\n%s", got)
	}
}

func TestRunCleanupCommandWithYes(t *testing.T) {
	m := &cleanupMock{dfOutput: "Images: 100MB\n"}
	var out bytes.Buffer
	opts := runtime.CleanupOptions{DryRun: false, Containers: true, Images: true, Volumes: true, Networks: true}

	if err := runCleanupCommand(context.Background(), m, opts, true, &out, strings.NewReader("n\n")); err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if len(m.cleanupCalls) != 2 {
		t.Fatalf("expected 2 cleanup calls (preview + real), got %d", len(m.cleanupCalls))
	}
	if !m.cleanupCalls[0].DryRun || m.cleanupCalls[1].DryRun {
		t.Errorf("first call should be dry-run preview, second real; got %+v", m.cleanupCalls)
	}
	got := out.String()
	if !strings.Contains(got, "3 would be removed") {
		t.Errorf("output missing preview counts, got:\n%s", got)
	}
	if !strings.Contains(got, "3 removed") {
		t.Errorf("output missing real-run counts, got:\n%s", got)
	}
	if !strings.Contains(got, "Proceed with cleanup") {
		t.Errorf("with --yes, confirm prompt should not appear, got:\n%s", got)
	}
}

func TestRunCleanupCommandCancelled(t *testing.T) {
	m := &cleanupMock{dfOutput: "Images: 100MB\n"}
	var out bytes.Buffer
	opts := runtime.CleanupOptions{DryRun: false, Containers: true, Images: true, Volumes: true, Networks: true}

	if err := runCleanupCommand(context.Background(), m, opts, false, &out, strings.NewReader("n\n")); err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if len(m.cleanupCalls) != 1 {
		t.Fatalf("cancelled run should only preview, got %d cleanup calls", len(m.cleanupCalls))
	}
	got := out.String()
	if !strings.Contains(got, "cleanup cancelled") {
		t.Errorf("expected cancellation message, got:\n%s", got)
	}
}

func TestConfirm(t *testing.T) {
	if !confirm(strings.NewReader("y\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('y') should be true")
	}
	if !confirm(strings.NewReader("Y\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('Y') should be true")
	}
	if !confirm(strings.NewReader("yes\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('yes') should be true")
	}
	if confirm(strings.NewReader("n\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('n') should be false")
	}
	if confirm(strings.NewReader(""), &bytes.Buffer{}, "? ") {
		t.Error("confirm(EOF) should be false")
	}
}

func TestPrintCleanupResult(t *testing.T) {
	var out bytes.Buffer
	res := runtime.CleanupResult{ContainersRemoved: 3, ImagesRemoved: 12, VolumesRemoved: 1, NetworksRemoved: 0}
	printCleanupResult(&out, res, true)
	got := out.String()
	for _, want := range []string{"3 would be removed", "12 would be removed", "1 would be removed", "0 would be removed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestConfirm|TestPrintCleanupResult" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: runCleanupCommand`

- [ ] **Step 3: Create `internal/cli/cmd_cleanup.go`**

```go
package cli

import (
	"bufio"
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
	Short: "Clean up unused Docker resources",
	Long: `Prunes stopped containers, dangling images, unused volumes, and unused
networks. Tengiz-managed objects (labeled tengiz-app / tengiz-env) are never
touched.

Use --dry-run to preview what would be removed without deleting anything.
Use --yes to skip the confirmation prompt (for CI/headless runs).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		return runCleanupCommand(cmd.Context(), rt, opts, yes, os.Stdout, os.Stdin)
	},
}

// cleanupOptionsFromFlags builds CleanupOptions from CLI flags. When no
// category flag is set, all categories are enabled.
func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if !containers && !images && !volumes && !networks {
		containers, images, volumes, networks = true, true, true, true
	}
	return runtime.CleanupOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
	}, nil
}

// cleanupRunner is the minimal runtime surface the cleanup command needs.
// It lets tests inject a small mock instead of a full runtime.Manager.
type cleanupRunner interface {
	Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error)
	DiskUsage(ctx context.Context) (string, error)
}

// runCleanupCommand runs the full cleanup flow. It is a separate function
// (rather than inline in RunE) so tests can inject a mock and buffers.
func runCleanupCommand(ctx context.Context, rt cleanupRunner, opts runtime.CleanupOptions, yes bool, out io.Writer, in io.Reader) error {
	before, err := rt.DiskUsage(ctx)
	if err != nil {
		return fmt.Errorf("disk usage: %w", err)
	}
	fmt.Fprintln(out, strings.TrimSuffix(before, "\n"))

	previewOpts := opts
	previewOpts.DryRun = true
	preview, err := rt.Cleanup(ctx, previewOpts)
	if err != nil {
		return fmt.Errorf("cleanup preview: %w", err)
	}
	printCleanupResult(out, preview, true)

	if opts.DryRun {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "[tengiz] dry-run complete — nothing was removed.")
		return nil
	}

	fmt.Fprintln(out, "")
	if !yes && !confirm(in, out, "Proceed with cleanup? [y/N] ") {
		fmt.Fprintln(out, "[tengiz] cleanup cancelled.")
		return nil
	}

	realOpts := opts
	realOpts.DryRun = false
	result, err := rt.Cleanup(ctx, realOpts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	printCleanupResult(out, result, false)

	after, err := rt.DiskUsage(ctx)
	if err == nil {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, strings.TrimSuffix(after, "\n"))
	}
	return nil
}

func printCleanupResult(out io.Writer, res runtime.CleanupResult, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	fmt.Fprintf(out, "[tengiz] containers: %d %s\n", res.ContainersRemoved, verb)
	fmt.Fprintf(out, "[tengiz] images:     %d %s\n", res.ImagesRemoved, verb)
	fmt.Fprintf(out, "[tengiz] volumes:    %d %s\n", res.VolumesRemoved, verb)
	fmt.Fprintf(out, "[tengiz] networks:   %d %s\n", res.NetworksRemoved, verb)
}

func confirm(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
```

- [ ] **Step 4: Register the command and its flags in `internal/cli/root.go`**

In `init()`, immediately after the line `rootCmd.AddCommand(volumeCmd)`, add:

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestConfirm|TestPrintCleanupResult" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build the whole module**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cmd_cleanup.go internal/cli/cmd_cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation + full verification

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: everything from Tasks 1-3 (verification only, no new code)

- [ ] **Step 1: Add a Features bullet to `README.md`**

In the Features list (after the "Deployment history" bullet at README.md:20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, dangling images, unused volumes, and unused networks while never touching deployed apps (protected by Tengiz labels).
```

- [ ] **Step 2: Add the command section to `README.md` CLI Reference**

After the `### tengiz rollback <app>` section (README.md:236), add:

```markdown
### `tengiz cleanup`

Prune unused Docker resources: stopped containers not managed by Tengiz, dangling (untagged) images, unused volumes, and unused networks.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--dry-run` | Show what would be removed without removing anything |
| `-y`, `--yes` | Skip the confirmation prompt (for CI/headless runs) |

With no category flag, all four categories are cleaned. Tengiz-managed objects (labeled `tengiz-app` / `tengiz-env`) are always protected and never removed. The command prints `docker system df` before and after cleanup. Use `--dry-run` to preview, then run again with `--yes` to perform the cleanup non-interactively.

Example:
```
tengiz cleanup --dry-run            # preview only
tengiz cleanup --containers --yes   # remove stopped non-Tengiz containers
tengiz cleanup                      # interactive: preview + confirm
```
```

- [ ] **Step 3: Add the command to `AGENTS.md` CLI list**

After the `tengiz rollback <app>` line in `AGENTS.md`, add:

```markdown
tengiz cleanup [--containers|--images|--volumes|--networks] [--dry-run] [-y] → prune unused Docker resources (never touches Tengiz-managed objects)
```

- [ ] **Step 4: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. (2026-08-14) |
```

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (except the known slow `proxy` TCP-timeout tests and time-sensitive `idle` tests, which may take a couple of seconds each — they must still PASS, not fail)

- [ ] **Step 6: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

- [ ] **1. Spec coverage** — spec (FUTURES_FEATURES.md #6) requires: label-based filtering protecting Tengiz-managed containers ✅ (Task 1 `label!=` filters, Task 2 impl); cleanup of unused volumes/networks/containers/images ✅ (Task 2 `cleanupVolumes/Networks/Containers/Images`); `tengiz cleanup` command ✅ (Task 3). Periodic/scheduled cleaning is out of scope (depends on feature #57 Background Monitoring Scheduler). Build-cache pruning deferred to feature #103 — explicitly declared in Global Constraints.
- [ ] **2. Placeholder scan** — no "TBD"/"TODO"/"implement later" placeholders; every code step contains complete code; no "add error handling" without code.
- [ ] **3. Type consistency** — `runtime.CleanupOptions` and `runtime.CleanupResult` field names (`DryRun`, `Containers`, `Images`, `Volumes`, `Networks` / `ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`) are identical in Task 1 (definition), Task 2 (implementation), and Task 3 (CLI usage). `runCleanupCommand` uses the small `cleanupRunner` interface consistently; `dockerRuntime`, `stubManager`, and the test mock all satisfy it via the same method signatures.