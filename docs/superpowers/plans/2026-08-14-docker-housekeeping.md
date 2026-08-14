# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes unused Docker resources (stopped containers, dangling images, unused volumes, unused networks) while always protecting Tengiz-managed containers via label-based filtering.

**Architecture:** A new `internal/cleanup` package exposes a `Manager` interface with a `Cleanup(ctx, opts) (*Report, error)` method. The docker-backed implementation shells out to the `docker` CLI via `os/exec` (matching the existing `internal/runtime` pattern): containers are listed with `docker ps --format`, parsed in Go, and any container labeled `tengiz-app=*` is skipped; images/volumes/networks use a deterministic before/after count diff around `docker image prune -f` / `docker volume prune -f` / `docker network prune -f`. A `tengiz cleanup` Cobra command with per-category flags (`--containers`, `--images`, `--volumes`, `--networks`; default = all) wires the manager to the CLI. Tests follow the existing stub + pure-helper pattern (no Docker daemon required in CI).

**Tech Stack:** Go 1.26, Cobra, `os/exec`, stdlib only (no new external dependencies).

## Global Constraints

- Tengiz-managed containers (label `tengiz-app=<app>`) must NEVER be removed — this is the core safety contract
- Cleanup is Docker-resource-only; it must not touch `~/.tengiz/` state files, build logs, or app config
- No `--env` flag needed — labels are env-independent, protection covers all environments
- Default behavior (no category flags) runs all four categories
- `docker image prune -f` removes only dangling images (no tag, not referenced by a container)
- `docker volume prune -f` removes only unused volumes (not referenced by any container)
- `docker network prune -f` removes only unused networks (not referenced by any container); built-in networks (bridge/host/none) are never touched by Docker itself
- All new tests must run without a Docker daemon (stub manager + pure-helper tests)
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` (Create) | `Options`, `Report`, `Manager` interface, `dockerCleaner` + `NewDocker()`, `stubManager` + `NewStub()`, and all exec-based cleanup logic |
| `internal/cleanup/cleanup_test.go` (Create) | Tests for stub manager, `staleContainerIDs`, `hasTengizLabel`, `countLines`, `diff`, and the no-category orchestrator path |
| `internal/cli/cleanup.go` (Create) | `tengiz cleanup` Cobra command, flags, `cleanupOptions(cmd)` helper, registration via local `init()` (matching `internal/cli/preview.go:83-87` — no change to `root.go` needed) |
| `internal/cli/cleanup_test.go` (Create) | Tests for `cleanupOptions()` flag parsing and command registration |
| `README.md` (Modify) | Add `### tengiz cleanup` section to CLI Reference after the `tengiz ps` section (line ~150) |
| `AGENTS.md` (Modify) | Add `tengiz cleanup` line to the CLI command block (after line 43) |
| `docs/FUTURES_FEATURES.md` (Modify) | Mark #6 Docker Housekeeping as ✅ Implemented (2026-08-14) in P0 table and the Implemented Features table |

Two new files created; five existing files modified.

---

### Task 1: Cleanup package foundation (types, interface, stub)

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (pure new package)
- Produces: `cleanup.Options{Containers, Images, Volumes, Networks bool}`, `cleanup.Report{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int}`, `cleanup.Manager` interface with `Cleanup(ctx context.Context, opts Options) (*Report, error)`, `cleanup.NewDocker() (Manager, error)`, `cleanup.NewStub() Manager`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"testing"
)

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), Options{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.VolumesRemoved != 0 || report.NetworksRemoved != 0 {
		t.Fatalf("Cleanup() = %+v, want empty report", *report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run 'TestStub' -v`
Expected: FAIL with `undefined: NewStub` / `undefined: Manager`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type Report struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}

type Manager interface {
	Cleanup(ctx context.Context, opts Options) (*Report, error)
}

type dockerCleaner struct{}

func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerCleaner{}, nil
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	return &Report{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run 'TestStub' -v`
Expected: PASS (both subtests)

- [ ] **Step 5: Run full package tests + vet**

Run: `go test ./... && go vet ./...`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup package with Manager interface and stub"
```

---

### Task 2: Container cleanup with Tengiz label protection

**Files:**
- Modify: `internal/cleanup/cleanup.go` (add imports `log`, `strings`; add `cleanupContainers`, `staleContainerIDs`, `hasTengizLabel`)
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.dockerCleaner` from Task 1
- Produces: `staleContainerIDs(output string) []string` (package-private), `hasTengizLabel(labels string) bool` (package-private), `cleanupContainers(ctx context.Context) (int, error)` on `*dockerCleaner` — later used by the orchestrator in Task 4

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestStaleContainerIDs(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "tengiz containers are protected",
			output: "abc123|tengiz-app=myapp,tengiz-env=production\n",
			want:   nil,
		},
		{
			name:   "exited foreign container",
			output: "def456|foo=bar\n",
			want:   []string{"def456"},
		},
		{
			name:   "mixed tengiz and foreign",
			output: "abc123|tengiz-app=myapp\ndef456|\nghi789|com.docker.compose.project=test\n",
			want:   []string{"def456", "ghi789"},
		},
		{
			name:   "tengiz label prefix collision is not protected",
			output: "jkl012|com.example.tengiz-app=x\n",
			want:   []string{"jkl012"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := staleContainerIDs(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("staleContainerIDs(%q) = %v, want %v", tt.output, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("staleContainerIDs(%q) = %v, want %v", tt.output, got, tt.want)
				}
			}
		})
	}
}

func TestHasTengizLabel(t *testing.T) {
	tests := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", true},
		{"tengiz-env=production", false},
		{"", false},
		{"com.example.tengiz-app=x", false},
	}
	for _, tt := range tests {
		if got := hasTengizLabel(tt.labels); got != tt.want {
			t.Errorf("hasTengizLabel(%q) = %v, want %v", tt.labels, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run 'TestStale|TestHasTengiz' -v`
Expected: FAIL with `undefined: staleContainerIDs` / `undefined: hasTengizLabel`

- [ ] **Step 3: Write minimal implementation**

Update the imports in `internal/cleanup/cleanup.go` to:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

Append to `internal/cleanup/cleanup.go`:

```go
func (c *dockerCleaner) cleanupContainers(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "status=exited",
		"--format", "{{.ID}}|{{.Labels}}").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := staleContainerIDs(string(out))
	removed := 0
	for _, id := range ids {
		if _, rmErr := exec.CommandContext(ctx, "docker", "rm", id).CombinedOutput(); rmErr != nil {
			log.Printf("[cleanup] failed to remove container %s: %v", id, rmErr)
			continue
		}
		removed++
	}
	return removed, nil
}

func staleContainerIDs(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if id == "" || hasTengizLabel(parts[1]) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func hasTengizLabel(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == "tengiz-app" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run 'TestStale|TestHasTengiz' -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Run full package tests + vet**

Run: `go test ./internal/cleanup/ && go vet ./internal/cleanup/`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: clean stopped containers while protecting tengiz-app labels"
```

---

### Task 3: Image, volume, and network cleanup via count diff

**Files:**
- Modify: `internal/cleanup/cleanup.go` (add `cleanupImages`, `imageCount`, `cleanupVolumes`, `volumeCount`, `cleanupNetworks`, `networkCount`, `countLines`, `diff`)
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.dockerCleaner` from Task 1
- Produces: `countLines(output string) int` (package-private), `diff(before, after int) int` (package-private), `cleanupImages(ctx) (int, error)`, `cleanupVolumes(ctx) (int, error)`, `cleanupNetworks(ctx) (int, error)` on `*dockerCleaner` — later used by the orchestrator in Task 4

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestCountLines(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{"empty", "", 0},
		{"whitespace only", "  \n\t\n", 0},
		{"single line", "abc\n", 1},
		{"multiple with blank", "a\n\nb\nc\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.output); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	if got := diff(3, 1); got != 2 {
		t.Errorf("diff(3,1) = %d, want 2", got)
	}
	if got := diff(1, 3); got != 0 {
		t.Errorf("diff(1,3) = %d, want 0", got)
	}
	if got := diff(2, 2); got != 0 {
		t.Errorf("diff(2,2) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run 'TestCountLines|TestDiff' -v`
Expected: FAIL with `undefined: countLines` / `undefined: diff`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
func (c *dockerCleaner) cleanupImages(ctx context.Context) (int, error) {
	before, err := c.imageCount(ctx)
	if err != nil {
		return 0, err
	}
	if err := exec.CommandContext(ctx, "docker", "image", "prune", "-f").Run(); err != nil {
		return 0, fmt.Errorf("docker image prune: %w", err)
	}
	after, err := c.imageCount(ctx)
	if err != nil {
		return 0, err
	}
	return diff(before, after), nil
}

func (c *dockerCleaner) imageCount(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "images", "-q").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return countLines(string(out)), nil
}

func (c *dockerCleaner) cleanupVolumes(ctx context.Context) (int, error) {
	before, err := c.volumeCount(ctx)
	if err != nil {
		return 0, err
	}
	if err := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").Run(); err != nil {
		return 0, fmt.Errorf("docker volume prune: %w", err)
	}
	after, err := c.volumeCount(ctx)
	if err != nil {
		return 0, err
	}
	return diff(before, after), nil
}

func (c *dockerCleaner) volumeCount(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "volume", "ls", "-q").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return countLines(string(out)), nil
}

func (c *dockerCleaner) cleanupNetworks(ctx context.Context) (int, error) {
	before, err := c.networkCount(ctx)
	if err != nil {
		return 0, err
	}
	if err := exec.CommandContext(ctx, "docker", "network", "prune", "-f").Run(); err != nil {
		return 0, fmt.Errorf("docker network prune: %w", err)
	}
	after, err := c.networkCount(ctx)
	if err != nil {
		return 0, err
	}
	return diff(before, after), nil
}

func (c *dockerCleaner) networkCount(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "network", "ls", "-q").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return countLines(string(out)), nil
}

func countLines(output string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func diff(before, after int) int {
	if d := before - after; d > 0 {
		return d
	}
	return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run 'TestCountLines|TestDiff' -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Run full package tests + vet**

Run: `go test ./internal/cleanup/ && go vet ./internal/cleanup/`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: prune dangling images, unused volumes, and unused networks"
```

---

### Task 4: Cleanup orchestrator with per-category options

**Files:**
- Modify: `internal/cleanup/cleanup.go` (add `Cleanup` method on `*dockerCleaner`; add `errors` import)
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.dockerCleaner.cleanupContainers`, `.cleanupImages`, `.cleanupVolumes`, `.cleanupNetworks` from Tasks 2-3
- Produces: `Cleanup(ctx context.Context, opts Options) (*Report, error)` on `*dockerCleaner`, completing the `Manager` interface — consumed by the CLI in Task 5

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestCleanupNoCategoriesRunsNoCommands(t *testing.T) {
	c := &dockerCleaner{}
	report, err := c.Cleanup(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	want := Report{}
	if *report != want {
		t.Fatalf("Cleanup() = %+v, want %+v", *report, want)
	}
}
```

This test runs with zero category flags set, so no `docker` commands are ever invoked — it is safe in CI without a Docker daemon.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run 'TestCleanupNoCategories' -v`
Expected: FAIL with `dockerCleaner does not implement Cleanup`

- [ ] **Step 3: Write minimal implementation**

Update the imports in `internal/cleanup/cleanup.go` to:

```go
import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

Append to `internal/cleanup/cleanup.go`:

```go
func (c *dockerCleaner) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	report := &Report{}
	var errs []error
	if opts.Containers {
		n, err := c.cleanupContainers(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("containers: %w", err))
		} else {
			report.ContainersRemoved = n
		}
	}
	if opts.Images {
		n, err := c.cleanupImages(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("images: %w", err))
		} else {
			report.ImagesRemoved = n
		}
	}
	if opts.Volumes {
		n, err := c.cleanupVolumes(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("volumes: %w", err))
		} else {
			report.VolumesRemoved = n
		}
	}
	if opts.Networks {
		n, err := c.cleanupNetworks(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("networks: %w", err))
		} else {
			report.NetworksRemoved = n
		}
	}
	return report, errors.Join(errs...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run 'TestCleanupNoCategories' -v`
Expected: PASS

- [ ] **Step 5: Run full package tests + vet**

Run: `go test ./internal/cleanup/ && go vet ./internal/cleanup/`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: orchestrate per-category cleanup with aggregated report"
```

---

### Task 5: CLI command `tengiz cleanup`

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.NewDocker() (Manager, error)`, `cleanup.Options`, `cleanup.Report` from Tasks 1-4
- Produces: `cleanupCmd *cobra.Command` (self-registers via its own `init()` following `internal/cli/preview.go:83-87`), `cleanupOptions(cmd *cobra.Command) cleanup.Options`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	return cmd
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	opts := cleanupOptions(newCleanupTestCmd())
	want := cleanup.Options{Containers: true, Images: true, Volumes: true, Networks: true}
	if opts != want {
		t.Fatalf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsSelectedCategories(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.Flags().Set("images", "true")
	cmd.Flags().Set("volumes", "true")
	opts := cleanupOptions(cmd)
	want := cleanup.Options{Containers: false, Images: true, Volumes: true, Networks: false}
	if opts != want {
		t.Fatalf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v`
Expected: FAIL with `undefined: cleanupOptions` and `cleanup command not found`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks)",
	Long: `Removes unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=*) are always protected and never removed.

Without category flags, all categories run:
  --containers  stopped containers not managed by Tengiz
  --images      dangling images (no tag, not referenced)
  --volumes     unused volumes (not referenced by any container)
  --networks    unused networks (not referenced by any container)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cleaner, err := cleanup.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := cleaner.Cleanup(cmd.Context(), cleanupOptions(cmd))
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Printf("  containers removed: %d\n", report.ContainersRemoved)
		fmt.Printf("  images removed: %d\n", report.ImagesRemoved)
		fmt.Printf("  volumes removed: %d\n", report.VolumesRemoved)
		fmt.Printf("  networks removed: %d\n", report.NetworksRemoved)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) cleanup.Options {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	if !containers && !images && !volumes && !networks {
		return cleanup.Options{Containers: true, Images: true, Volumes: true, Networks: true}
	}
	return cleanup.Options{Containers: containers, Images: images, Volumes: volumes, Networks: networks}
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Build the binary and verify the command runs**

Run: `go build -o tengiz . && ./tengiz cleanup --help`
Expected: usage text for `cleanup` with the four category flags and the `[tengiz]` help description

- [ ] **Step 6: Run full test suite + vet**

Run: `go test ./... -v -count=1 && go vet ./...`
Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 6: Documentation and feature status update

**Files:**
- Modify: `README.md:150` (insert `tengiz cleanup` section after the `tengiz ps` section)
- Modify: `AGENTS.md:43` (add `tengiz cleanup` line to CLI block)
- Modify: `docs/FUTURES_FEATURES.md:19` (P0 table #6 row) and `docs/FUTURES_FEATURES.md:253` (Implemented Features table)

**Interfaces:**
- Consumes: command name, flags, and behavior finalized in Task 5
- Produces: user-facing documentation describing `tengiz cleanup`

- [ ] **Step 1: Document the command in README.md**

Insert the following after the `### tengiz ps` section (which ends at line 150, immediately before `### tengiz logs`):

```markdown
### `tengiz cleanup [--containers] [--images] [--volumes] [--networks]`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images (no tag, not referenced) |
| `--volumes` | Remove unused volumes (not referenced by any container) |
| `--networks` | Remove unused networks (not referenced by any container) |

With no flags, all four categories run. Containers labeled `tengiz-app=*` (all Tengiz-managed apps) are always protected and never removed. A per-category count of removed resources is printed on completion.
```

- [ ] **Step 2: Document the command in AGENTS.md**

Insert the following line into the CLI code block after line 43 (`tengiz ps ...`):

```
tengiz cleanup [--containers|--images|--volumes|--networks] → remove unused Docker resources (Tengiz containers protected)
```

- [ ] **Step 3: Mark the feature implemented in docs/FUTURES_FEATURES.md**

In the P0 table, replace the #6 row (line 19) with:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Label-based prune to protect Tengiz containers. Disk space is the #1 production issue on single-server deployments. `tengiz cleanup`. |
```

In the Implemented Features table, add this row after the `Webhook ile Otomatik Deploy` row (line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

- [ ] **Step 4: Run full test suite + vet**

Run: `go test ./... -v -count=1 && go vet ./...`
Expected: all pass (docs changes only — no code behavior change)

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Manual Verification (post-implementation, requires Docker daemon)

After all tasks are merged, verify end-to-end on a host with Docker:

1. `go build -o tengiz .`
2. Deploy a throwaway app so a `tengiz-app` container exists, then stop it: `./tengiz deploy . && ./tengiz stop <app>`
3. Run `./tengiz cleanup` — confirm the stopped Tengiz container is NOT listed as removed, and the summary prints zero-counts or unrelated removals
4. Run `./tengiz cleanup --containers` — confirm only the non-Tengiz stopped containers are removed
5. Create a dangling image (`docker build` a no-tag build or `docker rmi` a tag) and run `./tengiz cleanup --images` — confirm the dangling image count decreases
6. Confirm `./tengiz cleanup` exits 0 and prints the four-line summary
