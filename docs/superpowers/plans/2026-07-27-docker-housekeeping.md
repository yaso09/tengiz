# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command family for label-aware Docker resource pruning, preventing disk exhaustion on single-server deployments.

**Architecture:** Extend `runtime.Manager` with `Prune*` methods that wrap `docker system prune` / `docker container prune` / `docker image prune` / `docker volume prune` / `docker network prune / docker builder prune`, always applying `--filter label=tengiz-app` to protect Tengiz-managed resources. Expose via `tengiz cleanup` CLI with `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all` flags.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI exec, existing `runtime.Manager` interface

## Global Constraints

- All prune operations MUST filter by `label!=tengiz-app` or equivalent to protect Tengiz containers
- Container names follow `tengiz-<appname>` / `tengiz-<appname>-<env>` convention
- `~/.tengiz/` state dir for any persistence
- `--env` flag respected for environment-scoped operations
- No new external dependencies
- All new `Manager` methods get stub implementations in `runtime.go`

---

### Task 1: Extend Manager Interface with Prune Methods

**Files:**
- Modify: `internal/runtime/runtime.go` (Manager interface + stub)
- Modify: `internal/runtime/cleanup.go` (dockerRuntime implementations)

**Interfaces:**
- Consumes: Existing `Manager` interface, `dockerRuntime` struct, `stubManager` struct
- Produces: `PruneContainers(ctx) (report, error)`, `PruneImages(ctx) (report, error)`, `PruneVolumes(ctx) (report, error)`, `PruneNetworks(ctx) (report, error)`, `PruneBuildCache(ctx) (report, error)` on `Manager`

- [ ] **Step 1: Add `PruneReport` type and new methods to `Manager` interface in `runtime.go`**

Add a shared result type and five new methods to `Manager`:

```go
type PruneReport struct {
	ReclaimedBytes uint64 `json:"reclaimed_bytes,omitempty"`
	ObjectsDeleted int    `json:"objects_deleted,omitempty"`
	Output         string `json:"output,omitempty"`
}

type Manager interface {
	// ... existing methods ...

	PruneContainers(ctx context.Context) (PruneReport, error)
	PruneImages(ctx context.Context) (PruneReport, error)
	PruneVolumes(ctx context.Context) (PruneReport, error)
	PruneNetworks(ctx context.Context) (PruneReport, error)
	PruneBuildCache(ctx context.Context) (PruneReport, error)
}
```

Edit `runtime.go` — insert `PruneReport` type before `Manager`, add the five methods to `Manager`:

- [ ] **Step 2: Add stub implementations in `runtime.go`**

```go
func (m *stubManager) PruneContainers(ctx context.Context) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneImages(ctx context.Context) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 3: Write failing tests for stub methods**

In `runtime_test.go` add:

```go
func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	report, err := m.PruneContainers(context.Background())
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if report.ObjectsDeleted != 0 {
		t.Errorf("ObjectsDeleted = %d, want 0", report.ObjectsDeleted)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	report, err := m.PruneImages(context.Background())
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if report.ObjectsDeleted != 0 {
		t.Errorf("ObjectsDeleted = %d, want 0", report.ObjectsDeleted)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	report, err := m.PruneVolumes(context.Background())
	if err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
	if report.ObjectsDeleted != 0 {
		t.Errorf("ObjectsDeleted = %d, want 0", report.ObjectsDeleted)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	report, err := m.PruneNetworks(context.Background())
	if err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
	if report.ObjectsDeleted != 0 {
		t.Errorf("ObjectsDeleted = %d, want 0", report.ObjectsDeleted)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	report, err := m.PruneBuildCache(context.Background())
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
	if report.ObjectsDeleted != 0 {
		t.Errorf("ObjectsDeleted = %d, want 0", report.ObjectsDeleted)
	}
}
```

- [ ] **Step 4: Run stub tests to verify they fail first**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL (new methods not in stubManager yet → compilation error before running, or if compiled: PASS because stubs return zero values)

- [ ] **Step 5: Add dockerRuntime implementations in `cleanup.go`**

Add helper and five implementations:

```go
func parsePruneOutput(out []byte) PruneReport {
	// Docker prune output format: "Total reclaimed space: 1.234GB\n"
	// or: "Deleted Containers:\n...\n\nTotal reclaimed space: 1.234GB\n"
	output := string(out)
	var reclaimed uint64
	re := regexp.MustCompile(`Total reclaimed space:\s+([0-9.]+)(kB|MB|GB|TB|B)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) == 3 {
		val, _ := strconv.ParseFloat(matches[1], 64)
		switch matches[2] {
		case "B":
			reclaimed = uint64(val)
		case "kB":
			reclaimed = uint64(val * 1024)
		case "MB":
			reclaimed = uint64(val * 1024 * 1024)
		case "GB":
			reclaimed = uint64(val * 1024 * 1024 * 1024)
		case "TB":
			reclaimed = uint64(val * 1024 * 1024 * 1024 * 1024)
		}
	}
	return PruneReport{
		ReclaimedBytes: reclaimed,
		Output:         output,
	}
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (PruneReport, error) {
	// Protect Tengiz containers: only prune stopped containers NOT labeled with tengiz-app
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) (PruneReport, error) {
	// Remove only dangling images (untagged <none>:<none>).
	// Avoids -a (all unused) because Tengiz images aren't labeled at build time
	// and cannot be safely filtered. Use KeepLastNImages per app for version cleanup.
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func countLinesStartingWith(out []byte, prefix string) int {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}
```

Add the missing imports to `cleanup.go`:
```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS (14+ tests)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/runtime/cleanup.go
git commit -m "feat(runtime): add Prune* methods to Manager interface with docker exec implementations"
```

---

### Task 2: CLI `tengiz cleanup` Command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` (register cleanupCmd)

**Interfaces:**
- Consumes: `runtime.Manager.PruneContainers`, `runtime.Manager.PruneImages`, `runtime.Manager.PruneVolumes`, `runtime.Manager.PruneNetworks`, `runtime.Manager.PruneBuildCache`
- Produces: `tengiz cleanup` CLI command

- [ ] **Step 1: Create `internal/cli/cleanup.go`**

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
	Short: "Free disk space by pruning unused Docker resources",
	Long: `Remove unused Docker containers, images, volumes, networks, and BuildKit cache.
Protects Tengiz-managed containers and images from accidental removal.

Examples:
  tengiz cleanup                           # prune everything
  tengiz cleanup --containers --images     # prune only containers and images
  tengiz cleanup --volumes                 # prune only unused volumes
  tengiz cleanup --all                     # same as default (all categories)
  tengiz cleanup --dry-run                 # show what would be removed (Docker outputs only)`,
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers (non-Tengiz)")
	cleanupCmd.Flags().Bool("images", false, "prune unused images (non-Tengiz)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune BuildKit build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all resource types")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned (runs Docker prune without -f)")

	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		// Default to all if no specific flag set
		if !containers && !images && !volumes && !networks && !buildCache {
			all = true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		ctx := context.Background()
		var totalReclaimed uint64

		printHeader := func(label string) {
			fmt.Printf("\n[cleanup] --- %s ---\n", label)
		}

		if all || containers {
			printHeader("Containers")
			report, err := rt.PruneContainers(ctx)
			if err != nil {
				fmt.Printf("[cleanup] container prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || images {
			printHeader("Images")
			report, err := rt.PruneImages(ctx)
			if err != nil {
				fmt.Printf("[cleanup] image prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || volumes {
			printHeader("Volumes")
			report, err := rt.PruneVolumes(ctx)
			if err != nil {
				fmt.Printf("[cleanup] volume prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || networks {
			printHeader("Networks")
			report, err := rt.PruneNetworks(ctx)
			if err != nil {
				fmt.Printf("[cleanup] network prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || buildCache {
			printHeader("Build Cache")
			report, err := rt.PruneBuildCache(ctx)
			if err != nil {
				fmt.Printf("[cleanup] build cache prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		fmt.Printf("\n[cleanup] total reclaimed: %s\n", formatBytes(totalReclaimed))
		return nil
	}
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2f kB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
```

- [ ] **Step 2: Register cleanup command in `root.go`**

In `root.go` `init()` function, add:
```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 3: Write CLI tests in `internal/cli/cleanup_test.go`**

Note: CLI tests that actually prunes are integration tests. We test the command registration and flag parsing:

```go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	// Verify the cleanup command is a child of root
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Use != "cleanup" {
		t.Errorf("expected Use=cleanup, got %q", cmd.Use)
	}
}

func TestCleanupFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	tests := []struct {
		name string
		flag string
	}{
		{"containers", "containers"},
		{"images", "images"},
		{"volumes", "volumes"},
		{"networks", "networks"},
		{"build-cache", "build-cache"},
		{"all", "all"},
		{"dry-run", "dry-run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := flags.Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not found", tt.flag)
			}
			if f.Value.Type() != "bool" {
				t.Errorf("flag %q type = %s, want bool", tt.flag, f.Value.Type())
			}
		})
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: PASS

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Build and verify compilation**

Run: `go build -o tengiz .`
Expected: binary compiles without errors

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command with per-category Docker resource pruning"
```

---

### Task 3: Add `KeepLastNContainers` for Stale Version Cleanup

**Files:**
- Modify: `internal/runtime/runtime.go` (Manager interface + stub)
- Modify: `internal/runtime/cleanup.go` (dockerRuntime implementation)

**Interfaces:**
- Consumes: Existing `Manager` interface, `ContainerName` helper
- Produces: `KeepLastNContainers(ctx, appName, n) error` on `Manager`

- [ ] **Step 1: Add method to `Manager` interface**

```go
KeepLastNContainers(ctx context.Context, appName string, n int) error
```

- [ ] **Step 2: Add stub in `runtime.go`**

```go
func (m *stubManager) KeepLastNContainers(ctx context.Context, appName string, n int) error {
	return nil
}
```

- [ ] **Step 3: Add dockerRuntime implementation in `cleanup.go`**

```go
func (r *dockerRuntime) KeepLastNContainers(ctx context.Context, appName string, n int) error {
	containerPrefix := ContainerName(appName, "")
	// List all containers matching the prefix (including env variants like tengiz-appname-dev)
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("name=tengiz-%s", appName),
		"--format", "{{.ID}}|{{.Names}}|{{.CreatedAt}}",
		"--no-trunc",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	// Parse lines into entries
	type entry struct {
		id    string
		name  string
		time  time.Time
	}
	var entries []entry
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
		if err != nil {
			continue
		}
		entries = append(entries, entry{id: parts[0], name: parts[1], time: t})
	}

	// Sort by creation time ascending (oldest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].time.Before(entries[j].time)
	})

	// Remove oldest containers beyond keep count
	for i := 0; i < len(entries)-n; i++ {
		cname := entries[i].name
		log.Printf("[runtime] removing old container %s", cname)
		rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", cname)
		if rmOut, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			log.Printf("[runtime] failed to remove old container %s: %v\n%s", cname, rmErr, string(rmOut))
		}
	}
	return nil
}
```

- [ ] **Step 4: Write unit test for stub**

In `runtime_test.go`:

```go
func TestStubKeepLastNContainers(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNContainers(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNContainers() error = %v", err)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/runtime/cleanup.go
git commit -m "feat(runtime): add KeepLastNContainers for stale version cleanup"
```

---

### Task 4: Integrate Notifications & Documentation

**Files:**
- Modify: `internal/cli/cleanup.go` (add notification on completion)
- Modify: `internal/types/types.go` (add `EventCleanup` event type)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 as implemented)

- [ ] **Step 1: Add `EventCleanup` notification type in `types/types.go`**

```go
const (
	EventCleanup NotificationEventType = "cleanup:completed"
)
```

- [ ] **Step 2: Send notification after cleanup in `cleanup.go`**

In the cleanup command's `RunE`, after all prunes, add:

```go
notifyMgr := notify.NewManager(dataDir, env)
if loadErr := notifyMgr.LoadConfig(); loadErr == nil {
	cfg := notifyMgr.GetConfig()
	if cfg != nil && cfg.Enabled {
		if cfg.Discord != nil {
			notifyMgr.AddNotifier(notify.NewDiscordNotifier(*cfg.Discord))
		}
		if cfg.Slack != nil {
			notifyMgr.AddNotifier(notify.NewSlackNotifier(*cfg.Slack))
		}
		if cfg.Email != nil {
			notifyMgr.AddNotifier(notify.NewEmailNotifier(*cfg.Email))
		}
	}
}

notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
	Type:    types.EventCleanup,
	Message: fmt.Sprintf("Docker cleanup completed. Total reclaimed: %s", formatBytes(totalReclaimed)),
	Metadata: map[string]string{
		"environment": env,
		"reclaimed":   fmt.Sprintf("%d", totalReclaimed),
	},
})
```

Need to add the `env` variable at top of `RunE`:
```go
env := getEnv(cmd)
```

- [ ] **Step 3: Run tests**

Run: `go build -o tengiz .`
Expected: compiles clean

Run: `go test ./... -count=1`
Expected: all tests pass

- [ ] **Step 4: Update FUTURES_FEATURES.md**

Change line for #6 from ⬜ to ✅ and add implementation date.

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also add to the Implemented Features section:
```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-27) |
```

- [ ] **Step 5: Final verification**

Run: `go vet ./...`
Expected: no warnings

Run: `go test ./... -count=1`
Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/types/types.go docs/FUTURES_FEATURES.md
git commit -m "feat(cleanup): integrate notifications and mark Docker Housekeeping implemented"
```
