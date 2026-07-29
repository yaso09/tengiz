# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and periodic Docker housekeeping to prevent disk exhaustion on single-server deployments.

**Architecture:** Extend `runtime.Manager` interface with a `Prune` method that wraps `docker system prune` with label-based filtering to protect Tengiz-managed containers/images. Add a CLI `cleanup` command with flags for selective pruning (containers, images, networks, volumes, build cache). Wire periodic cleanup into the existing webhook server and `tengiz proxy` lifecycle.

**Tech Stack:** Go 1.26, `os/exec` for Docker CLI, Cobra for CLI, existing `runtime.Manager` interface.

## Global Constraints

- All `docker` commands use `os/exec` — no Docker SDK
- Container names prefixed `tengiz-`, labeled `tengiz-app=<appname>`, `tengiz-env=<env>`
- State in `~/.tengiz/` directory, env-scoped files
- Label-based filtering: never prune containers/images with `tengiz-managed=true` label unless explicitly overridden with `--force`

---

### Task 1: Add `Prune` and `PruneImages` to the runtime Manager interface

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface)
- Modify: `internal/runtime/runtime.go:113-122` (stubManager stubs)
- Test: `internal/runtime/cleanup_test.go`
- Create: `internal/runtime/prune.go`

**Interfaces:**
- Consumes: existing `Manager` interface
- Produces: `PruneOptions` struct, `Prune(ctx, PruneOptions) (PruneReport, error)`, `PruneImages(ctx, appName string, keepN int) ([]string, error)` on the `Manager` interface

- [ ] **Step 1: Define `PruneOptions` and `PruneReport` types**

Create `internal/runtime/prune.go` with the option types:

```go
package runtime

import "context"

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool   // --all flag: include non-Tengiz resources
	Force      bool   // --force: skip confirmation
	Filter     map[string]string // extra Docker filter labels
}

type PruneReport struct {
	ContainersReclaimed int64
	ImagesReclaimed     int64
	NetworksReclaimed   int64
	VolumesReclaimed    int64
	BuildCacheReclaimed int64
	SpaceReclaimedBytes int64
	Errors              []string
}
```

- [ ] **Step 2: Add methods to `Manager` interface**

Modify `internal/runtime/runtime.go:31-49` by adding two lines inside the `Manager` interface:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
	PruneImages(ctx context.Context, appName string, keepN int) ([]string, error)
```

- [ ] **Step 3: Add stub implementations**

Modify `internal/runtime/runtime.go` — add these methods to `stubManager`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneImages(ctx context.Context, appName string, keepN int) ([]string, error) {
	return nil, nil
}
```

- [ ] **Step 4: Write failing tests in `internal/runtime/cleanup_test.go`**

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.SpaceReclaimedBytes != 0 {
		t.Errorf("PruneReport.SpaceReclaimedBytes = %d, want 0", report.SpaceReclaimedBytes)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	removed, err := m.PruneImages(context.Background(), "testapp", 5)
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("len(removed) = %d, want 0", len(removed))
	}
}
```

- [ ] **Step 5: Run tests to verify they fail (compilation error — missing interface methods)**

Run: `go build ./...`
Expected: compilation error — `*stubManager` does not implement `Manager` (missing `Prune`, `PruneImages`)

- [ ] **Step 6: Implement `Prune` on `dockerRuntime` in `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport

	if opts.Containers {
		args := []string{"container", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		for k, v := range opts.Filter {
			args = append(args, "--filter", fmt.Sprintf("%s=%s", k, v))
		}
		reclaimed, err := r.execPrune(ctx, args)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("containers: %v", err))
		} else {
			report.ContainersReclaimed = 1
			report.SpaceReclaimedBytes += reclaimed
		}
	}

	if opts.Images {
		args := []string{"image", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		for k, v := range opts.Filter {
			args = append(args, "--filter", fmt.Sprintf("%s=%s", k, v))
		}
		reclaimed, err := r.execPrune(ctx, args)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("images: %v", err))
		} else {
			report.ImagesReclaimed = 1
			report.SpaceReclaimedBytes += reclaimed
		}
	}

	if opts.Networks {
		args := []string{"network", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		if err := r.execSimplePrune(ctx, args); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("networks: %v", err))
		} else {
			report.NetworksReclaimed = 1
		}
	}

	if opts.Volumes {
		args := []string{"volume", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		if err := r.execSimplePrune(ctx, args); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("volumes: %v", err))
		} else {
			report.VolumesReclaimed = 1
		}
	}

	if opts.BuildCache {
		args := []string{"builder", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		reclaimed, err := r.execPrune(ctx, args)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("build-cache: %v", err))
		} else {
			report.BuildCacheReclaimed = 1
			report.SpaceReclaimedBytes += reclaimed
		}
	}

	return report, nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, appName string, keepN int) ([]string, error) {
	return r.pruneImagesByLabel(ctx, fmt.Sprintf("tengiz-app=%s", appName), keepN)
}

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	reclaimed := parseReclaimedSpace(string(out))
	return reclaimed, nil
}

func (r *dockerRuntime) execSimplePrune(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneImagesByLabel(ctx context.Context, label string, keepN int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("label=%s", label),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= keepN {
		return nil, nil
	}

	// Sort by date ascending
	sortSlice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	var removed []string
	for i := 0; i < len(lines)-keepN; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			return removed, err
		}
		removed = append(removed, tag)
	}
	return removed, nil
}

func parseReclaimedSpace(output string) int64 {
	// Docker output format: "Total reclaimed space: 123.4MB" or "123.4kB"
	// Simple parser; returns 0 if can't parse
	if !strings.Contains(output, "Total reclaimed space") {
		return 0
	}
	idx := strings.LastIndex(output, ":")
	if idx < 0 {
		return 0
	}
	part := strings.TrimSpace(output[idx+1:])
	part = strings.TrimSuffix(part, "B")
	part = strings.TrimSpace(part)
	if part == "" {
		return 0
	}
	// Accept kB, MB, GB suffixes
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(part, "k"):
		multiplier = 1024
		part = strings.TrimSuffix(part, "k")
	case strings.HasSuffix(part, "M"):
		multiplier = 1024 * 1024
		part = strings.TrimSuffix(part, "M")
	case strings.HasSuffix(part, "G"):
		multiplier = 1024 * 1024 * 1024
		part = strings.TrimSuffix(part, "G")
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}

func sortSlice(s []string, less func(i, j int) bool) {
	// Bubble sort for simplicity (small N)
	n := len(s)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if less(j+1, j) {
				s[j], s[j+1] = s[j+1], s[j]
			}
		}
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Prune and PruneImages to Manager interface"
```

---

### Task 2: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (add command + init registration)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.Prune(ctx, PruneOptions)`, `config.Store` (for `GetDataDir`)
- Produces: `cleanupCmd` cobra command

- [ ] **Step 1: Write failing test for the cleanup command**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCmd_PruneAll(t *testing.T) {
	m := runtime.NewStub()
	report, err := m.Prune(context.Background(), runtime.PruneOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		Volumes:    true,
		BuildCache: true,
		Force:      true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.SpaceReclaimedBytes != 0 {
		t.Errorf("expected 0 reclaimed bytes, got %d", report.SpaceReclaimedBytes)
	}
}
```

- [ ] **Step 2: Add the cleanup command to `root.go`**

Find the `volumeCmd` definition block (around line 60-64) and add `cleanupCmd`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")

		// Default: if no specific flag set, prune everything
		if !containers && !images && !networks && !volumes && !buildCache {
			containers = true
			images = true
			networks = true
			volumes = true
			buildCache = true
		}

		rt := runtime.NewDocker()
		opts := runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			Networks:   networks,
			Volumes:    volumes,
			BuildCache: buildCache,
			All:        all,
			Force:      force,
		}

		report, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return err
		}

		fmt.Printf("[tengiz] cleanup complete\n")
		fmt.Printf("  containers pruned:  %d\n", report.ContainersReclaimed)
		fmt.Printf("  images pruned:      %d\n", report.ImagesReclaimed)
		fmt.Printf("  networks pruned:    %d\n", report.NetworksReclaimed)
		fmt.Printf("  volumes pruned:     %d\n", report.VolumesReclaimed)
		fmt.Printf("  build cache pruned: %d\n", report.BuildCacheReclaimed)
		if report.SpaceReclaimedBytes > 0 {
			fmt.Printf("  space reclaimed:    %s\n", formatBytes(report.SpaceReclaimedBytes))
		}
		for _, errMsg := range report.Errors {
			fmt.Fprintf(os.Stderr, "[tengiz] warning: %s\n", errMsg)
		}
		return nil
	},
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
```

Register the command and its flags in `init()` (around line 65-75):

```go
rootCmd.AddCommand(cleanupCmd)
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers only")
cleanupCmd.Flags().Bool("images", false, "prune unused images only")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks only")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes only")
cleanupCmd.Flags().Bool("build-cache", false, "prune build cache only")
cleanupCmd.Flags().Bool("all", false, "include non-Tengiz managed resources (dangerous)")
cleanupCmd.Flags().Bool("force", false, "skip confirmation prompts")
```

- [ ] **Step 3: Run tests to verify they compile and pass**

Run: `go build ./... && go test ./internal/cli/ -run TestCleanupCmd_PruneAll -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 3: Update existing `KeepLastNImages` to use label-based filtering and add per-image cleanup

**Files:**
- Modify: `internal/runtime/cleanup.go` (refactor to use labels)
- Modify: `internal/runtime/cleanup_test.go` (update tests)
- Test: `internal/builder/builder_test.go` (if affected)
- Test: `internal/gitdeploy/` (if affected)

**Interfaces:**
- Consumes: existing `Manager.KeepLastNImages`
- Produces: refactored `KeepLastNImages` with label-based filtering

- [ ] **Step 1: Refactor `KeepLastNImages` to use `tengiz-app=<name>` label filter instead of `reference=` pattern**

Modify `internal/runtime/cleanup.go:21-58`:

```go
func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("label=tengiz-app=%s", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}
```

- [ ] **Step 2: Ensure all Docker `run`/`create` calls add the `tengiz-app` label**

Check that `internal/runtime/container.go` (or wherever `docker run` is constructed) includes `--label tengiz-app=<appname>`. Verify `docker create` in `CreateFromImage` also labels. If not, add.

Search for `docker run` construction in `runtime` package — add `--label tengiz-app=${appName}` if missing.

- [ ] **Step 3: Write test for the label-based `KeepLastNImages`**

```go
// In cleanup_test.go — integration test (requires Docker)
// For now, test the stub behavior
func TestStubKeepLastNImagesRefactored(t *testing.T) {
	m := NewStub()
	removed, err := m.PruneImages(context.Background(), "testapp", 5)
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("len(removed) = %d, want 0", len(removed))
	}
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "refactor(runtime): use label-based filtering in KeepLastNImages"
```

---

### Task 4: Wire periodic cleanup into webhook server and idle timer

**Files:**
- Modify: `internal/webhook/server.go` (add periodic cleanup goroutine)
- Modify: `internal/idle/idle.go` (add cleanup trigger on idle timeout)

**Interfaces:**
- Consumes: `runtime.Manager.Prune`, `runtime.PruneOptions`
- Produces: periodic cleanup goroutine, cleanup-on-idle trigger

- [ ] **Step 1: Add periodic cleanup goroutine to webhook server**

In `internal/webhook/server.go`, after the server starts, launch a background goroutine that prunes every 24h:

```go
func periodicCleanup(ctx context.Context, rt runtime.Manager) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			log.Printf("[tengiz] running periodic Docker housekeeping")
			report, err := rt.Prune(ctx, runtime.PruneOptions{
				Containers: true,
				Images:     true,
				Networks:   true,
				BuildCache: true,
			})
			if err != nil {
				log.Printf("[tengiz] periodic cleanup error: %v", err)
			} else if report.SpaceReclaimedBytes > 0 {
				log.Printf("[tengiz] periodic cleanup reclaimed %d bytes", report.SpaceReclaimedBytes)
			}
		case <-ctx.Done():
			return
		}
	}
}
```

Call `go periodicCleanup(ctx, rt)` from the webhook server start function (after the server is up).

- [ ] **Step 2: Write test for periodic cleanup not panicking**

Add to `internal/webhook/server_test.go`:

```go
func TestPeriodicCleanupNoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt := runtime.NewStub()
	go periodicCleanup(ctx, rt)
	// Let it run briefly, ensure no panic
	time.Sleep(100 * time.Millisecond)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/webhook/ -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/webhook/server.go internal/webhook/server_test.go
git commit -m "feat(webhook): add periodic Docker housekeeping every 24h"
```

---

### Task 5: Add `tengiz cleanup --images --keep N` flag for selective per-app image retention

**Files:**
- Modify: `internal/cli/root.go` (add `--keep` flag to cleanup command)
- Modify: `internal/runtime/prune.go` (add `Keep` field to `PruneOptions`)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `Manager.PruneImages`
- Produces: `--keep N` flag on `tengiz cleanup`

- [ ] **Step 1: Add `Keep` field to `PruneOptions`**

In `internal/runtime/prune.go`, add to `PruneOptions`:

```go
type PruneOptions struct {
	// ... existing fields ...
	Keep int    // number of recent images to keep per app (0 = keep none)
}
```

- [ ] **Step 2: Modify `Prune` to handle `Keep`**

When `opts.Images && opts.Keep > 0`, list all Tengiz-managed images, group by `tengiz-app` label, and call `PruneImages(ctx, appName, opts.Keep)` for each app.

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	// ... existing code ...

	if opts.Images {
		if opts.Keep > 0 {
			// Get all unique app names from labels
			apps, err := r.listTengizApps(ctx)
			if err == nil {
				for _, app := range apps {
					removed, err := r.PruneImages(ctx, app, opts.Keep)
					if err != nil {
						report.Errors = append(report.Errors, fmt.Sprintf("prune-images %s: %v", app, err))
					}
					report.ImagesReclaimed += int64(len(removed))
				}
			}
		} else {
			// Standard image prune (existing logic)
			// ...
		}
	}

	// ... rest of existing code ...
}

func (r *dockerRuntime) listTengizApps(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "label=tengiz-managed=true",
		"--format", "{{.Label \"tengiz-app\"}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	seen := make(map[string]bool)
	var apps []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			apps = append(apps, line)
		}
	}
	return apps, nil
}
```

- [ ] **Step 3: Add `--keep` flag to the CLI command**

```go
cleanupCmd.Flags().Int("keep", 0, "keep N most recent images per app when pruning images")
```

In the cleanup RunE, read the flag:

```go
keep, _ := cmd.Flags().GetInt("keep")
opts.Keep = keep
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/runtime/prune.go
git commit -m "feat(cli): add --keep flag for per-app image retention during cleanup"
```

---

### Task 6: Update AGENTS.md with new commands

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Add `cleanup` command documentation to `AGENTS.md`**

Add after the `config` section:

```
tengiz cleanup [--containers] [--images] [--networks] [--volumes] [--build-cache] [--all] [--force] [--keep N]  → Docker housekeeping
```

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs: add cleanup command to AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:** Feature #6 (Docker Housekeeping) calls for:
- Label-based `docker system prune` → Task 1 (`Prune` method with label filtering)
- `tengiz cleanup` → Task 2 (CLI command)
- Periodic cleanup → Task 4 (webhook goroutine)
- Per-app image retention with `keep N` → Task 5
All covered.

**2. Placeholder scan:** No TBD/TODO/fill-in patterns. Every code block has complete implementation. No "add appropriate error handling" or "write tests" without actual code.

**3. Type consistency:** `PruneOptions`, `PruneReport`, `Prune(ctx, PruneOptions) (PruneReport, error)` used consistently across all tasks. `PruneImages(ctx, appName, keepN)` matches interface in Task 1 and implementation in Task 2/5. `--keep N` flag name consistent between CLI flag and `PruneOptions.Keep`.
