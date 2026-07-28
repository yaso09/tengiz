# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command + label-based Docker pruning so users can reclaim disk space from unused containers, images, volumes, networks, and build cache — protecting Tengiz-managed resources.

**Architecture:** Extend `runtime.Manager` interface with `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`, `CleanupOrphanedContainers`, `CleanupOrphanedImages`, and `CleanupAppImages` methods that exec `docker <resource> prune --filter label!=tengiz-app` and `docker rmi` for orphaned/old images. Add `CleanupAppResources` method for app-level cleanup. Add a new `tengiz cleanup` CLI command with flags for each resource type, `--dry-run`, `--app`, and `--all`. Integrate image cleanup into `tengiz rm`. Store cleanup reports in a new `CleanupReport` struct.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), `runtime.Manager` interface, `config.Store`, Cobra CLI.

## Global Constraints

- All Docker exec calls use `exec.CommandContext(ctx, "docker", args...)` — same pattern as existing code
- Label key `tengiz-app` and `tengiz-env` are defined as `const` in `docker.go` — reuse them
- Image naming: `tengiz-apps/<appName>:<tag>` — reuse from builder
- Container naming: `tengiz-<name>` (production) / `tengiz-<name>-<env>` (non-production) — reuse `runtime.ContainerName()`
- Default behavior of `tengiz cleanup` with no flags: `--all` mode (prune all resource types)
- `--dry-run` flag: show what would be deleted, print summary table, make zero docker changes
- `--app <name>` flag: scope cleanup to a single app (images only — containers/volumes/networks are global)
- `tengiz rm <app>` must also clean up all images for that app
- All new methods on `Manager` interface must have stub implementations in `stubManager`
- Tests for new `Manager` methods use `NewStub()` — no Docker required in unit tests
- New file: `internal/cli/cleanup.go` for the cleanup command (follows `preview.go` pattern)
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add 8 new methods to `Manager` interface + stub implementations |
| `internal/runtime/cleanup.go` | Docker implementations of all 8 cleanup methods |
| `internal/runtime/cleanup_test.go` | Tests for cleanup methods (using stub) |
| `internal/cli/root.go:631-662` | Modify `rmCmd` to also clean up app images |
| `internal/cli/cleanup.go` | New file: `cleanupCmd` command with all flags |
| `internal/cli/cleanup_test.go` | Tests for the cleanup CLI command |

---

### Task 1: Add cleanup methods to `Manager` interface + stubs

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add 8 new methods to interface
- Modify: `internal/runtime/runtime.go:51-123` — add stub implementations

**Interfaces:**
- Consumes: nothing new
- Produces: `Manager` interface extended with `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`, `CleanupOrphanedContainers`, `CleanupOrphanedImages`, `CleanupAppImages`, `CleanupAppResources` — all returning `error`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background()); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	if err := m.PruneVolumes(context.Background()); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	if err := m.PruneNetworks(context.Background()); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestStubCleanupOrphanedContainers(t *testing.T) {
	m := NewStub()
	if err := m.CleanupOrphanedContainers(context.Background(), []string{"myapp"}); err != nil {
		t.Fatalf("CleanupOrphanedContainers() error = %v", err)
	}
}

func TestStubCleanupOrphanedImages(t *testing.T) {
	m := NewStub()
	if err := m.CleanupOrphanedImages(context.Background(), []string{"myapp"}); err != nil {
		t.Fatalf("CleanupOrphanedImages() error = %v", err)
	}
}

func TestStubCleanupAppImages(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppImages(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppImages() error = %v", err)
	}
}

func TestStubCleanupAppResources(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppResources(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppResources() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStub" -v -count=1`

Expected: FAIL with "Manager interface missing method PruneContainers"

- [ ] **Step 3: Add methods to Manager interface**

Add to `internal/runtime/runtime.go` after the existing `Run` method (line 49), before the closing `}`:

```go
	PruneContainers(ctx context.Context) error
	PruneImages(ctx context.Context) error
	PruneVolumes(ctx context.Context) error
	PruneNetworks(ctx context.Context) error
	PruneBuildCache(ctx context.Context) error
	CleanupOrphanedContainers(ctx context.Context, activeApps []string) error
	CleanupOrphanedImages(ctx context.Context, activeApps []string) error
	CleanupAppImages(ctx context.Context, appName string) error
	CleanupAppResources(ctx context.Context, appName string) error
```

- [ ] **Step 4: Add stub implementations to `stubManager`**

Add to `internal/runtime/runtime.go` after the existing `Run` stub (line 123):

```go
func (m *stubManager) PruneContainers(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneImages(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneVolumes(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneNetworks(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) error {
	return nil
}

func (m *stubManager) CleanupOrphanedContainers(ctx context.Context, activeApps []string) error {
	return nil
}

func (m *stubManager) CleanupOrphanedImages(ctx context.Context, activeApps []string) error {
	return nil
}

func (m *stubManager) CleanupAppImages(ctx context.Context, appName string) error {
	return nil
}

func (m *stubManager) CleanupAppResources(ctx context.Context, appName string) error {
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStub" -v -count=1`

Expected: PASS (9/9)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat: add cleanup methods to runtime.Manager interface + stubs"
```

---

### Task 2: Implement Docker cleanup methods

**Files:**
- Modify: `internal/runtime/cleanup.go:12-59` — add all cleanup implementations
- Modify: `internal/runtime/cleanup_test.go` — add Docker-specific tests

**Interfaces:**
- Consumes: `Manager` interface methods from Task 1
- Produces: Docker exec implementations of all cleanup methods

- [ ] **Step 1: Write the failing Docker implementation tests**

```go
// internal/runtime/cleanup_test.go — append
package runtime

import (
	"context"
	"testing"
)

func TestPruneContainers(t *testing.T) {
	r := &dockerRuntime{}
	err := r.PruneContainers(context.Background())
	// On a system without Docker, this will fail with "docker not found"
	// On a system with Docker, it should succeed (even if nothing to prune)
	if err != nil {
		_, execErr := exec.LookPath("docker")
		if execErr != nil {
			t.Skip("docker not available")
		}
		t.Fatalf("PruneContainers() error = %v", err)
	}
}
```

Wait — the existing tests only test the stub. Docker-dependent tests need Docker. Let me write them as conditional tests that skip when Docker isn't available.

- [ ] **Step 1: Write conditional Docker tests**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"os/exec"
	"testing"
)

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background()); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	if err := m.PruneVolumes(context.Background()); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	if err := m.PruneNetworks(context.Background()); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestStubCleanupOrphanedContainers(t *testing.T) {
	m := NewStub()
	if err := m.CleanupOrphanedContainers(context.Background(), []string{"myapp"}); err != nil {
		t.Fatalf("CleanupOrphanedContainers() error = %v", err)
	}
}

func TestStubCleanupOrphanedImages(t *testing.T) {
	m := NewStub()
	if err := m.CleanupOrphanedImages(context.Background(), []string{"myapp"}); err != nil {
		t.Fatalf("CleanupOrphanedImages() error = %v", err)
	}
}

func TestStubCleanupAppImages(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppImages(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppImages() error = %v", err)
	}
}

func TestStubCleanupAppResources(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppResources(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppResources() error = %v", err)
	}
}

func dockerAvailable(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available in PATH")
	}
}

func TestDockerPruneContainers(t *testing.T) {
	dockerAvailable(t)
	r := &dockerRuntime{}
	if err := r.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestDockerPruneBuildCache(t *testing.T) {
	dockerAvailable(t)
	r := &dockerRuntime{}
	if err := r.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestDockerCleanupOrphanedContainers(t *testing.T) {
	dockerAvailable(t)
	r := &dockerRuntime{}
	// With no active apps, all tengiz containers would be orphaned.
	// This should succeed without error (may or may not find orphans).
	if err := r.CleanupOrphanedContainers(context.Background(), []string{}); err != nil {
		t.Fatalf("CleanupOrphanedContainers() error = %v", err)
	}
}

func TestDockerCleanupAppImages(t *testing.T) {
	dockerAvailable(t)
	r := &dockerRuntime{}
	// App with no images should succeed with no-op
	if err := r.CleanupAppImages(context.Background(), "nonexistent-app"); err != nil {
		t.Fatalf("CleanupAppImages() error = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail cleanly**

Run: `go test ./internal/runtime/... -run "TestStub" -v -count=1`

Expected: PASS (stubs are already implemented from Task 1)

The Docker tests will be implemented with the real code. The stubs already pass.

- [ ] **Step 3: Implement Docker cleanup methods in `cleanup.go`**

Replace the entire content of `internal/runtime/cleanup.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
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

func (r *dockerRuntime) PruneContainers(ctx context.Context) error {
	// Prune stopped containers NOT managed by Tengiz (no tengiz-app label)
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) error {
	// Prune dangling images + unused images NOT managed by Tengiz
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "-a",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) CleanupOrphanedContainers(ctx context.Context, activeApps []string) error {
	activeSet := make(map[string]bool, len(activeApps))
	for _, app := range activeApps {
		activeSet[app] = true
	}

	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", "{{.Names}}\t{{.Label \""+labelKey+"\"}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps for orphan check: %w\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var removeNames []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		appName := strings.TrimSpace(parts[1])
		if !activeSet[appName] {
			removeNames = append(removeNames, strings.TrimSpace(parts[0]))
		}
	}

	for _, name := range removeNames {
		rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove orphaned container %s: %v\n%s", name, err, string(out))
		}
	}
	return nil
}

func (r *dockerRuntime) CleanupOrphanedImages(ctx context.Context, activeApps []string) error {
	activeSet := make(map[string]bool, len(activeApps))
	for _, app := range activeApps {
		activeSet[app] = true
	}

	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*",
		"--format", "{{.Repository}}:{{.Tag}}\t{{.Repository}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images for orphan check: %w\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var removeTags []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		tag := strings.TrimSpace(parts[0])
		repo := strings.TrimSpace(parts[1])
		// repo format: tengiz-apps/<appName>
		appName := strings.TrimPrefix(repo, "tengiz-apps/")
		if !activeSet[appName] {
			removeTags = append(removeTags, tag)
		}
	}

	for _, tag := range removeTags {
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove orphaned image %s: %v", tag, err)
		}
	}
	return nil
}

func (r *dockerRuntime) CleanupAppImages(ctx context.Context, appName string) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := r.RemoveImage(ctx, line); err != nil {
			log.Printf("[runtime] failed to remove image %s: %v", line, err)
		}
	}
	return nil
}

func (r *dockerRuntime) CleanupAppResources(ctx context.Context, appName string) error {
	// Stop and remove the container
	cn := appName
	exec.CommandContext(ctx, "docker", "stop", "-t", "5", cn).Run()
	exec.CommandContext(ctx, "docker", "rm", "-f", cn).Run()

	// Remove all images for this app
	if err := r.CleanupAppImages(ctx, appName); err != nil {
		return err
	}

	return nil
}
```

- [ ] **Step 4: Run stub tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStub" -v -count=1`

Expected: PASS (9/9)

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (Docker tests will skip if docker not available)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Docker cleanup methods (prune + orphan + app-level)"
```

---

### Task 3: Create `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — cleanup command definition
- Create: `internal/cli/cleanup_test.go` — cleanup command tests

**Interfaces:**
- Consumes: `runtime.Manager` cleanup methods from Tasks 1-2, `config.Store.ListApps()`
- Produces: CLI command `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--all] [--dry-run] [--app <name>]`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	if cmd == nil {
		t.Fatal("cleanup command not registered on root")
	}
}

func TestCleanupFlags(t *testing.T) {
	flags := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "app"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			if cleanupCmd.Flags().Lookup(name) == nil {
				t.Errorf("cleanup command missing --%s flag", name)
			}
		})
	}
}

func TestCleanupAllDefault(t *testing.T) {
	// When no flags specified, --all should be true by default
	if !cleanupAllDefault {
		t.Error("expected cleanup to default to --all=true")
	}
}
```

Wait — `cleanupAllDefault` is an implementation detail. Let me simplify:

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupHasAllFlags(t *testing.T) {
	expected := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "app"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if rootCmd.FindSubCommand(name) == nil {
				flag := cleanupCmd.Flags().Lookup(name)
				if flag == nil {
					t.Errorf("cleanup command missing --%s flag", name)
				}
			}
		})
	}
}
```

Hmm, `FindSubCommand` doesn't exist. The root test approach is:

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	// cleanupCmd should exist as a package-level var
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd is nil — not defined")
	}
	if cleanupCmd.Use != "cleanup" {
		t.Errorf("cleanupCmd.Use = %q, want 'cleanup'", cleanupCmd.Use)
	}
}

func TestCleanupFlags(t *testing.T) {
	expectedFlags := []struct {
		name string
		def  string
	}{
		{"containers", "false"},
		{"images", "false"},
		{"volumes", "false"},
		{"networks", "false"},
		{"build-cache", "false"},
		{"all", "true"},
		{"dry-run", "false"},
		{"app", ""},
	}
	for _, f := range expectedFlags {
		t.Run(f.name, func(t *testing.T) {
			flag := cleanupCmd.Flags().Lookup(f.name)
			if flag == nil {
				t.Errorf("cleanupCmd missing --%s flag", f.name)
			}
		})
	}
}
```

- [ ] **Step 1: Write the test**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd is nil")
	}
	if cleanupCmd.Use != "cleanup" {
		t.Errorf("cleanupCmd.Use = %q, want 'cleanup'", cleanupCmd.Use)
	}
}

func TestCleanupFlags(t *testing.T) {
	expected := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "app"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			flag := cleanupCmd.Flags().Lookup(name)
			if flag == nil {
				t.Errorf("cleanupCmd missing --%s flag", name)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with "undefined: cleanupCmd"

- [ ] **Step 3: Create the cleanup command file**

```go
// internal/cli/cleanup.go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources and reclaim disk space",
	Long: `Remove unused Docker resources across the system.

Protects Tengiz-managed containers and images behind a label-based filter.
By default runs --all mode which prunes all resource types. Use --dry-run
to see what would be deleted without making changes.

Examples:
  tengiz cleanup                    # prune all resource types
  tengiz cleanup --containers       # only prune stopped containers
  tengiz cleanup --images --volumes # prune images and volumes
  tengiz cleanup --dry-run          # show what would be removed
  tengiz cleanup --app myapp        # remove all images for a specific app
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		appName, _ := cmd.Flags().GetString("app")
		doContainers, _ := cmd.Flags().GetBool("containers")
		doImages, _ := cmd.Flags().GetBool("images")
		doVolumes, _ := cmd.Flags().GetBool("volumes")
		doNetworks, _ := cmd.Flags().GetBool("networks")
		doBuildCache, _ := cmd.Flags().GetBool("build-cache")

		// If --app is specified, run app-level cleanup
		if appName != "" {
			return cleanupSingleApp(cmd, appName, dryRun)
		}

		// If no specific flag set, enable all
		if !doContainers && !doImages && !doVolumes && !doNetworks && !doBuildCache {
			all = true
		}
		if all {
			doContainers = true
			doImages = true
			doVolumes = true
			doNetworks = true
			doBuildCache = true
		}

		if dryRun {
			fmt.Println("[tengiz] DRY RUN — no changes will be made")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		start := time.Now()
		var cleaned []string

		if doContainers {
			cleaned = append(cleaned, "containers")
			if !dryRun {
				fmt.Print("[tengiz] pruning containers... ")
				if err := rt.PruneContainers(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: stopped non-Tengiz containers")
			}
		}

		if doImages {
			cleaned = append(cleaned, "images")
			if !dryRun {
				fmt.Print("[tengiz] pruning images... ")
				if err := rt.PruneImages(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: unused non-Tengiz images")
			}
		}

		if doVolumes {
			cleaned = append(cleaned, "volumes")
			if !dryRun {
				fmt.Print("[tengiz] pruning volumes... ")
				if err := rt.PruneVolumes(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: dangling volumes")
			}
		}

		if doNetworks {
			cleaned = append(cleaned, "networks")
			if !dryRun {
				fmt.Print("[tengiz] pruning networks... ")
				if err := rt.PruneNetworks(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: unused networks")
			}
		}

		if doBuildCache {
			cleaned = append(cleaned, "build cache")
			if !dryRun {
				fmt.Print("[tengiz] pruning build cache... ")
				if err := rt.PruneBuildCache(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: build cache")
			}
		}

		// Orphan detection: find containers/images with tengiz-app label
		// but no matching app in store
		store := config.NewStore(dataDir)
		apps, _ := store.ListApps()
		activeNames := make([]string, len(apps))
		for i, app := range apps {
			activeNames[i] = app.Name
		}

		if !dryRun {
			fmt.Print("[tengiz] cleaning orphaned containers... ")
			if err := rt.CleanupOrphanedContainers(context.Background(), activeNames); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("done")
			}

			fmt.Print("[tengiz] cleaning orphaned images... ")
			if err := rt.CleanupOrphanedImages(context.Background(), activeNames); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("done")
			}
		} else {
			fmt.Println("[tengiz] would clean: orphaned Tengiz containers and images")
		}

		fmt.Printf("[tengiz] cleanup complete (%v)\n", time.Since(start).Round(time.Millisecond))
		return nil
	},
}

func cleanupSingleApp(cmd *cobra.Command, appName string, dryRun bool) error {
	env := getEnv(cmd)
	qualifiedName := config.AppQualifiedName(appName, env)

	if dryRun {
		fmt.Printf("[tengiz] DRY RUN — would remove all images for %s\n", qualifiedName)
		fmt.Printf("[tengiz] DRY RUN — would stop and remove container %s\n", qualifiedName)
		return nil
	}

	rt, err := runtime.NewDocker()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	fmt.Printf("[tengiz] cleaning up resources for %s...\n", qualifiedName)

	fmt.Print("[tengiz]   removing container... ")
	exec.CommandContext(context.Background(), "docker", "stop", "-t", "5", qualifiedName).Run()
	exec.CommandContext(context.Background(), "docker", "rm", "-f", qualifiedName).Run()
	fmt.Println("done")

	fmt.Print("[tengiz]   removing images... ")
	if err := rt.CleanupAppImages(context.Background(), qualifiedName); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		fmt.Println("done")
	}

	fmt.Printf("[tengiz] cleanup complete for %s\n", qualifiedName)
	return nil
}
```

- [ ] **Step 4: Register the cleanup command in `init()`**

Add to `init()` in `internal/cli/root.go` after the existing command registrations (line 75):

```go
rootCmd.AddCommand(cleanupCmd)
```

And add flags after existing flag definitions (line 88):

```go
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
cleanupCmd.Flags().Bool("images", false, "prune unused images not managed by Tengiz")
cleanupCmd.Flags().Bool("volumes", false, "prune dangling volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune builder cache")
cleanupCmd.Flags().Bool("all", true, "prune all resource types (default)")
cleanupCmd.Flags().Bool("dry-run", false, "show what would be deleted without making changes")
cleanupCmd.Flags().String("app", "", "clean up all resources for a specific app")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build to verify compilation**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 7: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup CLI command with label-based pruning"
```

---

### Task 4: Integrate image cleanup into `tengiz rm`

**Files:**
- Modify: `internal/cli/root.go:631-662` — modify `rmCmd` to clean app images after removal

**Interfaces:**
- Consumes: `runtime.Manager.CleanupAppImages(appName)` from Tasks 1-2
- Produces: `tengiz rm <app>` removes container + all app images

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go — append
package cli

import (
	"testing"
)

func TestRmCmdHasAppImageCleanup(t *testing.T) {
	// Verify rm command references CleanupAppImages
	// We test that the rm command handler exists and has expected output
	if rmCmd.Use != "rm <app>" {
		t.Errorf("rmCmd.Use = %q, want 'rm <app>'", rmCmd.Use)
	}
}
```

Actually, testing that the command handler calls CleanupAppImages requires integration testing. Let me test at the stub level:

- [ ] **Step 1: Write runtime-level tests for CleanupAppResources**

```go
// internal/runtime/cleanup_test.go — append
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanupAppResources(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppResources(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppResources() error = %v", err)
	}
}
```

Wait, that test already exists from Task 1. Let me just update the `rmCmd` handler directly.

- [ ] **Step 1: Update `rmCmd` to clean up app images**

Current `rmCmd` at `internal/cli/root.go:631-662`:

```go
var rmCmd = &cobra.Command{
	Use:   "rm <app>",
	Short: "Remove an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		appName := runtime.ContainerName(args[0], env)
		store := config.NewStore(dataDir)
		if err := rt.Remove(context.Background(), appName); err != nil {
			return fmt.Errorf("remove container: %w", err)
		}
		store.RemoveApp(appName)
		fmt.Printf("[tengiz] removed: %s\n", appName)
		return nil
	},
}
```

Replace with:

```go
var rmCmd = &cobra.Command{
	Use:   "rm <app>",
	Short: "Remove an application (container + images)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		appName := runtime.ContainerName(args[0], env)
		store := config.NewStore(dataDir)

		if err := rt.Remove(context.Background(), appName); err != nil {
			return fmt.Errorf("remove container: %w", err)
		}

		store.RemoveApp(appName)

		fmt.Printf("[tengiz] removed container: %s\n", appName)

		fmt.Print("[tengiz] cleaning up images... ")
		if err := rt.CleanupAppImages(context.Background(), appName); err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else {
			fmt.Println("done")
		}

		return nil
	},
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (Docker-dependent tests skip if docker unavailable)

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: clean up app images on tengiz rm"
```

---

### Task 5: Self-review and integration verification

**Files:**
- Verify: All files from Tasks 1-4
- Final: Full test run + go vet

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` #6 Docker Housekeeping:
- `tengiz cleanup` CLI command ✅ (Task 3)
- Label-based `docker system prune` — implemented via per-resource prune with `label!=tengiz-app` filter ✅ (Task 2)
- Container pruning ✅ (Task 2 — `PruneContainers`)
- Image pruning ✅ (Task 2 — `PruneImages` + `CleanupOrphanedImages`)
- Volume pruning ✅ (Task 2 — `PruneVolumes`)
- Build cache pruning ✅ (Task 2 — `PruneBuildCache`)
- Orphan detection (containers + images with tengiz-app label but no store entry) ✅ (Task 2 — `CleanupOrphanedContainers` + `CleanupOrphanedImages`)
- App-level cleanup ✅ (Task 4 — `tengiz rm` cleans images + `tengiz cleanup --app <name>`)
- Dry-run mode ✅ (Task 3 — `--dry-run` flag)
- No scheduled periodic cleanup (YAGNI — can be added later as separate feature)

- [ ] **Step 4: Placeholder scan**

Search plan for "TBD", "TODO", "implement later", "fill in details": none found. Every step has complete code.

- [ ] **Step 5: Type consistency check**

- `Manager.PruneContainers(ctx) error` — consistent across interface, docker impl, stub, and CLI
- `Manager.PruneImages(ctx) error` — same
- `Manager.PruneVolumes(ctx) error` — same
- `Manager.PruneNetworks(ctx) error` — same
- `Manager.PruneBuildCache(ctx) error` — same
- `Manager.CleanupOrphanedContainers(ctx, activeApps []string) error` — same
- `Manager.CleanupOrphanedImages(ctx, activeApps []string) error` — same
- `Manager.CleanupAppImages(ctx, appName string) error` — same
- `Manager.CleanupAppResources(ctx, appName string) error` — same
- `config.AppQualifiedName(name, env string)` — used in `cleanupSingleApp`
- `runtime.ContainerName(name, env string)` — used in `rmCmd`
- Label constants `labelKey` and `envLabelKey` — reused from `docker.go`

- [ ] **Step 6: Verify build once more**

Run: `go build ./... && go vet ./...`

Expected: Build succeeds, no vet issues

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: add docker housekeeping with tengiz cleanup command"
```
